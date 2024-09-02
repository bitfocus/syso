package rsrc

import (
	"encoding/binary"
	"io"
	"unicode/utf16"

	"github.com/bitfocus/syso/pkg/coff"
	"github.com/bitfocus/syso/pkg/common"
	"github.com/pkg/errors"
)

// Section represents the .rsrc section in PE file.
type Section struct {
	rootDir     *Directory
	relocations []coff.Relocation
}

// New returns an empty .rsrc section.
func New() *Section {
	return &Section{
		rootDir: &Directory{},
	}
}

// Name returns the section's name, ".rsrc".
func (s *Section) Name() string {
	return ".rsrc"
}

// Size returns the section's size.
func (s *Section) Size() int {
	return int(s.freeze())
}

// Relocations returns relocations that should be applied to the section.
func (s *Section) Relocations() []coff.Relocation {
	s.freeze()
	return s.relocations
}

// ResourceIDExists returns true if a resource with given integer id exists.
func (s *Section) ResourceIDExists(id int) bool {
	for _, e := range s.rootDir.idEntries {
		if e.subdirectory != nil {
			for _, e2 := range e.subdirectory.idEntries {
				if *e2.id == id {
					return true
				}
			}
		}
	}
	return false
}

// ResourceNameExists returns true if a resource with given name exists.
func (s *Section) ResourceNameExists(name string) bool {
	for _, e := range s.rootDir.idEntries {
		if e.subdirectory != nil {
			for _, e2 := range e.subdirectory.nameEntries {
				if e2.name.string == name {
					return true
				}
			}
		}
	}
	return false
}

// AddResourceByID adds resource blob with arbitrary type identified by
// an integer id into the section.
func (s *Section) AddResourceByID(typ, id int, blob common.Blob) error {
	if _, err := s.addResource(typ, &id, nil, blob); err != nil {
		return err
	}
	return nil
}

// AddResourceByName adds resource blob with arbitrary type identified by
// a name into the section.
func (s *Section) AddResourceByName(typ int, name string, blob common.Blob) error {
	if _, err := s.addResource(typ, nil, &name, blob); err != nil {
		return err
	}
	return nil
}

func (s *Section) addResource(typ int, id *int, name *string, blob common.Blob) (*DataEntry, error) {
	var subdir *Directory
	var err error

	for _, e := range s.rootDir.idEntries {
		if *e.id == typ {
			if e.subdirectory == nil {
				return nil, errors.New("subdirectory should exist")
			}
			subdir = e.subdirectory
		}
	}
	if subdir == nil {
		subdir, err = s.rootDir.addSubdirectory(nil, &typ, 0)
		if err != nil {
			return nil, errors.Wrap(err, "failed to add `id` level resource directory")
		}
	}

	if id != nil {
		subdir, err = subdir.addSubdirectory(nil, id, 0)
	} else {
		subdir, err = subdir.addSubdirectory(name, nil, 0)
	}
	if err != nil {
		return nil, errors.Wrap(err, "failed to add `language` level subdirectory")
	}

	lang := enUSLanguage
	d, err := subdir.addData(nil, &lang, blob)
	if err != nil {
		return nil, errors.Wrap(err, "failed to add resource data")
	}

	return d, nil
}

func (s *Section) freeze() uint32 {
	var offset uint32

	s.relocations = nil

	s.rootDir.walk(func(dir *Directory) error {
		// TODO: instead of calculating size of newly created dummy structure,
		// use fixed constant.
		dir.offset = offset
		offset += uint32(binary.Size(&rawDirectory{}))
		for _, e := range dir.entries() {
			e.offset = offset
			offset += uint32(binary.Size(&rawDirectoryEntry{}))
		}
		return nil
	})

	s.rootDir.walk(func(dir *Directory) error {
		for _, str := range dir.strings {
			str.offset = offset
			// TODO: should we encode string to calculate its utf-16
			// encoded size? better solution may exist.
			offset += 2 + uint32(binary.Size(utf16.Encode([]rune(str.string))))
		}
		return nil
	})

	s.rootDir.walk(func(dir *Directory) error {
		for _, e := range dir.dataEntries() {
			e.offset = offset
			s.relocations = append(s.relocations, &Relocation{
				va: offset,
			})
			offset += uint32(binary.Size(&rawDataEntry{}))
		}
		return nil
	})

	s.rootDir.walk(func(dir *Directory) error {
		for _, d := range dir.datas() {
			d.offset = offset
			offset += uint32(d.Size())
		}
		return nil
	})

	return offset
}

// WriteTo writes section data to w.
func (s *Section) WriteTo(w io.Writer) (int64, error) {
	var written int64

	s.freeze()

	if err := s.rootDir.walk(func(dir *Directory) error {
		n, err := common.BinaryWriteTo(w, &rawDirectory{
			Characteristics:     dir.characteristics,
			NumberOfNameEntries: uint16(len(dir.nameEntries)),
			NumberOfIDEntries:   uint16(len(dir.idEntries)),
		})
		if err != nil {
			return errors.Wrap(err, "failed to write resource directory")
		}
		written += n

		for i, e := range dir.entries() {
			var id uint32
			if e.name != nil {
				id = e.name.offset | 0x80000000
			} else {
				id = uint32(*e.id)
			}
			var o uint32
			if e.dataEntry != nil {
				o = e.dataEntry.offset
			} else {
				o = e.subdirectory.offset | 0x80000000
			}
			n, err := common.BinaryWriteTo(w, &rawDirectoryEntry{
				NameOffsetOrIntegerID:               id,
				DataEntryOffsetOrSubdirectoryOffset: o,
			})
			if err != nil {
				return errors.Wrapf(err, "failed to write resource directory entry #%d", i)
			}
			written += n
		}

		return nil
	}); err != nil {
		return written, err
	}

	if err := s.rootDir.walk(func(dir *Directory) error {
		for _, str := range dir.strings {
			u := utf16.Encode([]rune(str.string))
			n, err := common.BinaryWriteTo(w, uint16(len(u)))
			if err != nil {
				return errors.Wrapf(err, "failed to write resource string(%q)'s length(%d)", str.string, len(u))
			}
			written += n
			n, err = common.BinaryWriteTo(w, u)
			if err != nil {
				return errors.Wrapf(err, "failed to write resource string(%q)", str.string)
			}
			written += n
		}
		return nil
	}); err != nil {
		return written, err
	}

	if err := s.rootDir.walk(func(dir *Directory) error {
		for i, e := range dir.dataEntries() {
			n, err := common.BinaryWriteTo(w, &rawDataEntry{
				DataRVA: e.data.offset,
				Size:    uint32(e.data.Size()),
			})
			if err != nil {
				return errors.Wrapf(err, "failed to write resource data entry #%d", i)
			}
			written += n
		}
		return nil
	}); err != nil {
		return written, err
	}

	if err := s.rootDir.walk(func(dir *Directory) error {
		for i, d := range dir.datas() {
			n, err := io.CopyN(w, d, d.Size())
			if err != nil {
				return errors.Wrapf(err, "failed to write resource data #%d", i)
			}
			written += n
		}
		return nil
	}); err != nil {
		return written, err
	}

	return written, nil
}

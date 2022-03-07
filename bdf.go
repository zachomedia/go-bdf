package bdf

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"image"
	"strconv"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
	"golang.org/x/text/encoding/charmap"
)

type Character struct {
	Name       string
	Encoding   rune
	Advance    [2]int
	Alpha      *image.Alpha
	LowerPoint [2]int
}

type Font struct {
	Name        string
	Size        int
	PixelSize   int
	DPI         [2]int
	BPP         int
	Ascent      int
	Descent     int
	CapHeight   int
	XHeight     int
	Characters  []Character
	CharMap     map[rune]*Character
	Encoding    string
	DefaultChar rune
}

type Face struct {
	Font *Font
}

func (f *Font) NewFace() font.Face {
	return &Face{
		Font: f,
	}
}

func (f *Font) lookup(r rune) *Character {
	c, ok := f.CharMap[r]
	if !ok {
		c, ok = f.CharMap[f.DefaultChar]
		if !ok {
			return nil
		}
	}
	return c
}

func parseGlobalsAndProperties(s *bufio.Scanner, f *Font) error {
	var err error

	var registry string
	var encoding string
	var defaultChar int

scan:
	for s.Scan() {
		components := strings.Split(s.Text(), " ")
		switch components[0] {
		case "FONT":
			f.Name = components[1]
		case "SIZE":
			f.Size, err = strconv.Atoi(components[1])
			if err != nil {
				return err
			}

			f.DPI[0], err = strconv.Atoi(components[2])
			if err != nil {
				return err
			}

			f.DPI[1], err = strconv.Atoi(components[3])
			if err != nil {
				return err
			}

			if len(components) > 4 {
				f.BPP, err = strconv.Atoi(components[4])
				if err != nil {
					return err
				}
			}
		case "CHARSET_REGISTRY":
			registry = components[1]
		case "CHARSET_ENCODING":
			encoding = components[1]
		case "PIXEL_SIZE":
			f.PixelSize, err = strconv.Atoi(components[1])
		case "FONT_ASCENT":
			f.Ascent, err = strconv.Atoi(components[1])
			if err != nil {
				return err
			}
		case "FONT_DESCENT":
			f.Descent, err = strconv.Atoi(components[1])
			if err != nil {
				return err
			}
		case "CAP_HEIGHT":
			f.CapHeight, err = strconv.Atoi(components[1])
			if err != nil {
				return err
			}
		case "X_HEIGHT":
			f.XHeight, err = strconv.Atoi(components[1])
			if err != nil {
				return err
			}
		case "DEFAULT_CHAR":
			defaultChar, err = strconv.Atoi(components[1])
			if err != nil {
				return err
			}
		case "CHARS":
			count, err := strconv.Atoi(components[1])
			if err != nil {
				return err
			}
			f.Characters = make([]Character, count)
			break scan
		}
	}

	f.Encoding = registry + "-" + encoding
	f.DefaultChar = charToRune(f.Encoding, defaultChar)

	return nil
}

func charToRune(encoding string, char int) rune {
	if charMap := findCharmap(encoding); charMap != nil {
		return charMap.DecodeByte(byte(char))
	}
	return rune(char)
}

func findCharmap(requested string) *charmap.Charmap {
	trimmed := strings.TrimSpace(strings.ToLower(requested))

	knownMaps := map[string]*charmap.Charmap{
		"iso8859-1":  charmap.ISO8859_1,
		"iso8859-2":  charmap.ISO8859_2,
		"iso8859-9":  charmap.ISO8859_9,
		"iso8859-15": charmap.ISO8859_15,
	}

	charMap := knownMaps[trimmed]
	return charMap
}

func bitAt(xs []byte, i int) byte {
	return (xs[i>>3] >> (7 - (i % 8))) & 1
}

func Parse(data []byte) (*Font, error) {
	r := bytes.NewReader(data)
	s := bufio.NewScanner(r)

	f := Font{
		CharMap:     make(map[rune]*Character),
		DefaultChar: 32,
		BPP:         1,
	}

	var err error

	err = parseGlobalsAndProperties(s, &f)
	if err != nil {
		return nil, err
	}

	charMap := findCharmap(f.Encoding)

	char := -1
	row := -1
	inBitmap := false
	for s.Scan() {
		components := strings.Split(s.Text(), " ")

		if !inBitmap {
			switch components[0] {

			case "STARTCHAR":
				char++
				f.Characters[char].Name = components[1]
			case "ENCODING":
				code, err := strconv.Atoi(components[1])
				if err != nil {
					return nil, err
				}

				var r rune
				if charMap != nil {
					r = charMap.DecodeByte(byte(code))
				} else {
					r = rune(code)
				}
				f.Characters[char].Encoding = r
				f.CharMap[r] = &f.Characters[char]
			case "DWIDTH":
				f.Characters[char].Advance[0], err = strconv.Atoi(components[1])
				if err != nil {
					return nil, err
				}

				f.Characters[char].Advance[1], err = strconv.Atoi(components[2])
				if err != nil {
					return nil, err
				}
			case "BBX":
				w, err := strconv.Atoi(components[1])
				if err != nil {
					return nil, err
				}

				h, err := strconv.Atoi(components[2])
				if err != nil {
					return nil, err
				}

				// Lower-left corner?
				lx, err := strconv.Atoi(components[3])
				if err != nil {
					return nil, err
				}
				ly, err := strconv.Atoi(components[4])
				if err != nil {
					return nil, err
				}

				f.Characters[char].LowerPoint[0] = lx
				f.Characters[char].LowerPoint[1] = ly

				f.Characters[char].Alpha = &image.Alpha{
					Stride: w,
					Rect: image.Rectangle{
						Max: image.Point{
							X: w,
							Y: h,
						},
					},
					Pix: make([]byte, w*h),
				}
			case "BITMAP":
				inBitmap = true
				row = -1
			}
		} else {
			if components[0] == "ENDCHAR" {
				inBitmap = false
				continue
			}

			row = row + 1
			b, err := hex.DecodeString(s.Text())
			if err != nil {
				return nil, err
			}

			for i := 0; i < f.Characters[char].Alpha.Stride; i++ {
				val := byte(0x00)
				for j := 0; j < f.BPP; j++ {
					val <<= 1
					val |= bitAt(b, i*f.BPP+j)
				}
				f.Characters[char].Alpha.Pix[row*f.Characters[char].Alpha.Stride+i] = byte(uint32(val) * 0xff / ((1 << f.BPP) - 1))
			}
		}
	}

	return &f, nil
}

func (f *Face) Close() error { return nil }

func (f *Face) Metrics() font.Metrics {
	return font.Metrics{
		Ascent:    fixed.I(f.Font.Ascent),
		Descent:   fixed.I(f.Font.Descent),
		CapHeight: fixed.I(f.Font.CapHeight),
		XHeight:   fixed.I(f.Font.XHeight),
		Height:    fixed.I(f.Font.Ascent + f.Font.Descent),
	}
}

func (f *Face) Kern(_, _ rune) fixed.Int26_6 {
	return 0
}

func (f *Face) Glyph(dot fixed.Point26_6, r rune) (dr image.Rectangle, mask image.Image, maskp image.Point, advance fixed.Int26_6, ok bool) {
	c := f.Font.lookup(r)
	if c == nil {
		return image.Rectangle{}, nil, image.Point{}, 0, false
	}

	mask = c.Alpha

	x := int(dot.X)>>6 + c.LowerPoint[0]
	y := int(dot.Y)>>6 - c.LowerPoint[1]
	dr = image.Rectangle{
		Min: image.Point{
			X: x,
			Y: y - c.Alpha.Rect.Max.Y,
		},
		Max: image.Point{
			X: x + c.Alpha.Stride,
			Y: y,
		},
	}

	return dr, mask, image.Point{Y: 0}, fixed.I(c.Advance[0]), true
}

func (f *Face) GlyphBounds(r rune) (bounds fixed.Rectangle26_6, advance fixed.Int26_6, ok bool) {
	c := f.Font.lookup(r)
	if c == nil {
		return fixed.R(0, -f.Font.Ascent, 0, +f.Font.Descent), 0, false
	}

	return fixed.R(c.LowerPoint[0], -f.Font.Ascent, c.LowerPoint[0]+c.Alpha.Rect.Dx(), f.Font.Descent), fixed.I(c.Advance[0]), true
}

func (f *Face) GlyphAdvance(r rune) (advance fixed.Int26_6, ok bool) {
	c := f.Font.lookup(r)
	if c == nil {
		return 0, false
	}
	return fixed.I(c.Advance[0]), true
}

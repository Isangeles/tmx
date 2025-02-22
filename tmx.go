/*
   Copyright (c) Utkan Güngördü <utkan@freeconsole.org>, Dariusz Sikora <ds@isangeles.dev>

   This program is free software; you can redistribute it and/or modify
   it under the terms of the GNU General Public License as
   published by the Free Software Foundation; either version 3 or
   (at your option) any later version.

   This program is distributed in the hope that it will be useful,
   but WITHOUT ANY WARRANTY; without even the implied warranty of

   MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the

   GNU General Public License for more details


   You should have received a copy of the GNU General Public
   License along with this program; if not, write to the
   Free Software Foundation, Inc.,
   51 Franklin Street, Fifth Floor, Boston, MA  02110-1301, USA.
*/

// A Go library that reads Tiled's TMX files.
package tmx

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"io"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
)

const (
	GIDHorizontalFlip = 0x80000000
	GIDVerticalFlip   = 0x40000000
	GIDDiagonalFlip   = 0x20000000
	GIDFlip           = GIDHorizontalFlip | GIDVerticalFlip | GIDDiagonalFlip
	GIDMask           = 0x0fffffff
)

var (
	UnknownEncoding       = errors.New("tmx: invalid encoding scheme")
	UnknownCompression    = errors.New("tmx: invalid compression method")
	InvalidDecodedDataLen = errors.New("tmx: invalid decoded data length")
	InvalidGID            = errors.New("tmx: invalid GID")
	InvalidPointsField    = errors.New("tmx: invalid points string")
)

var (
	NilTile = &DecodedTile{Nil: true}
)

type GID uint32 // A tile ID. Could be used for GID or ID.
type ID uint32

// All structs have their fields exported, and you'll be on the safe side as long as treat them read-only (anyone want to write 100 getters?).
type Map struct {
	Version      string        `xml:"title,attr" json:"title"`
	Orientation  string        `xml:"orientation,attr" json:"orientation"`
	Width        int           `xml:"width,attr" json:"width"`
	Height       int           `xml:"height,attr" json:"height"`
	TileWidth    int           `xml:"tilewidth,attr" json:"tilewidth"`
	TileHeight   int           `xml:"tileheight,attr" json:"tileheight"`
	Properties   []Property    `xml:"properties>property" json:"properties"`
	Tilesets     []Tileset     `xml:"tileset" json:"tilesets"`
	Layers       []Layer       `xml:"layer" json:"layers"`
	ObjectGroups []ObjectGroup `xml:"objectgroup" json:"objectgroup"`
}

type Tileset struct {
	FirstGID   GID        `xml:"firstgid,attr" json:"firstgid"`
	Source     string     `xml:"source,attr" json:"source"`
	Name       string     `xml:"name,attr" json:"name"`
	TileWidth  int        `xml:"tilewidth,attr" json:"tilewidth"`
	TileHeight int        `xml:"tileheight,attr" json:"tileheight"`
	Spacing    int        `xml:"spacing,attr" json:"spacing"`
	Margin     int        `xml:"margin,attr" json:"margin"`
	Properties []Property `xml:"properties>property" json:"properties"`
	Image      Image      `xml:"image" json:"image"`
	Tiles      []Tile     `xml:"tile" json:"tile"`
	Tilecount  int        `xml:"tilecount,attr" json:"tilecount"`
	Columns    int        `xml:"columns,attr" json:"colums"`
}

type Image struct {
	Source string `xml:"source,attr" json:"source"`
	Trans  string `xml:"trans,attr" json:"trans"`
	Width  int    `xml:"width,attr" json:"width"`
	Height int    `xml:"height,attr" json:"height"`
}

type Tile struct {
	ID    ID    `xml:"id,attr" json:"id"`
	Image Image `xml:"image" json:"image"`
}

type Layer struct {
	Name         string         `xml:"name,attr" json:"name"`
	Opacity      float32        `xml:"opacity,attr" json:"opacity"`
	Visible      bool           `xml:"visible,attr" json:"visible"`
	Properties   []Property     `xml:"properties>property" json:"properties"`
	Data         Data           `xml:"data" json:"data"`
	DecodedTiles []*DecodedTile `xml:"-" json:"-"` // This is the attiribute you'd like to use, not Data. Tile entry at (x,y) is obtained using l.DecodedTiles[y*map.Width+x].
	Tileset      *Tileset       `xml:"-" json:"-"` // This is only set when the layer uses a single tileset and NilLayer is false.
	Empty        bool           `xml:"-" json:"-"` // Set when all entries of the layer are NilTile
}

type Data struct {
	Encoding    string     `xml:"encoding,attr" json:"encoding"`
	Compression string     `xml:"compression,attr" json:"compression"`
	RawData     []byte     `xml:",innerxml" json:"innerxml"`
	DataTiles   []DataTile `xml:"tile" json:"tile"` // Only used when layer encoding is xml
}

type ObjectGroup struct {
	Name       string     `xml:"name,attr" json:"name"`
	Color      string     `xml:"color,attr" json:"color"`
	Opacity    float32    `xml:"opacity,attr" json:"opacity"`
	Visible    bool       `xml:"visible,attr" json:"visible"`
	Properties []Property `xml:"properties>property" json:"properties"`
	Objects    []Object   `xml:"object" json:"object"`
}

type Object struct {
	Name       string     `xml:"name,attr" json:"name"`
	Type       string     `xml:"type,attr" json:"type"`
	X          float64    `xml:"x,attr" json:"x"`
	Y          float64    `xml:"y,attr" json:"y"`
	Width      float64    `xml:"width,attr" json:"width"`
	Height     float64    `xml:"height,attr" json:"height"`
	GID        int        `xml:"gid,attr" json"gid"`
	Visible    bool       `xml:"visible,attr" json:"visible"`
	Polygons   []Polygon  `xml:"polygon" json:"polygon"`
	PolyLines  []PolyLine `xml:"polyline" json:"polyline"`
	Properties []Property `xml:"properties>property" json:"properties"`
}

type Polygon struct {
	Points string `xml:"points,attr" json:"points"`
}

type PolyLine struct {
	Points string `xml:"points,attr" json:"points"`
}

type Property struct {
	Name  string `xml:"name,attr" json:"name"`
	Value string `xml:"value,attr" json:"value"`
}

func (d *Data) decodeBase64() (data []byte, err error) {
	rawData := bytes.TrimSpace(d.RawData)
	r := bytes.NewReader(rawData)

	encr := base64.NewDecoder(base64.StdEncoding, r)

	var comr io.Reader
	switch d.Compression {
	case "gzip":
		comr, err = gzip.NewReader(encr)
		if err != nil {
			return
		}
	case "zlib":
		comr, err = zlib.NewReader(encr)
		if err != nil {
			return
		}
	case "":
		comr = encr
	default:
		err = UnknownCompression
		return
	}

	return ioutil.ReadAll(comr)
}

func (d *Data) decodeCSV() (data []GID, err error) {
	cleaner := func(r rune) rune {
		if (r >= '0' && r <= '9') || r == ',' {
			return r
		}
		return -1
	}
	rawDataClean := strings.Map(cleaner, string(d.RawData))

	str := strings.Split(string(rawDataClean), ",")

	gids := make([]GID, len(str))
	for i, s := range str {
		var d uint64
		d, err = strconv.ParseUint(s, 10, 32)
		if err != nil {
			return
		}
		gids[i] = GID(d)
	}
	return gids, err
}

func (m *Map) decodeLayerXML(l *Layer) (gids []GID, err error) {
	if len(l.Data.DataTiles) != m.Width*m.Height {
		return []GID{}, InvalidDecodedDataLen
	}

	gids = make([]GID, len(l.Data.DataTiles))
	for i := 0; i < len(gids); i++ {
		gids[i] = l.Data.DataTiles[i].GID
	}

	return gids, nil
}

func (m *Map) decodeLayerCSV(l *Layer) ([]GID, error) {
	gids, err := l.Data.decodeCSV()
	if err != nil {
		return []GID{}, err
	}

	if len(gids) != m.Width*m.Height {
		return []GID{}, InvalidDecodedDataLen
	}

	return gids, nil
}

func (m *Map) decodeLayerBase64(l *Layer) ([]GID, error) {
	dataBytes, err := l.Data.decodeBase64()
	if err != nil {
		return []GID{}, err
	}

	if len(dataBytes) != m.Width*m.Height*4 {
		return []GID{}, InvalidDecodedDataLen
	}

	gids := make([]GID, m.Width*m.Height)

	j := 0
	for y := 0; y < m.Height; y++ {
		for x := 0; x < m.Width; x++ {
			gid := GID(dataBytes[j]) +
				GID(dataBytes[j+1])<<8 +
				GID(dataBytes[j+2])<<16 +
				GID(dataBytes[j+3])<<24
			j += 4

			gids[y*m.Width+x] = gid
		}
	}

	return gids, nil
}

func (m *Map) decodeLayer(l *Layer) ([]GID, error) {
	switch l.Data.Encoding {
	case "csv":
		return m.decodeLayerCSV(l)
	case "base64":
		return m.decodeLayerBase64(l)
	case "": // XML "encoding"
		return m.decodeLayerXML(l)
	}
	return []GID{}, UnknownEncoding
}

func (m *Map) decodeLayers() (err error) {
	for i := 0; i < len(m.Layers); i++ {
		l := &m.Layers[i]
		var gids []GID
		if gids, err = m.decodeLayer(l); err != nil {
			return err
		}

		l.DecodedTiles = make([]*DecodedTile, len(gids))
		for j := 0; j < len(l.DecodedTiles); j++ {
			l.DecodedTiles[j], err = m.DecodeGID(gids[j])
			if err != nil {
				return err
			}
		}
	}
	return nil
}

type Point struct {
	X int
	Y int
}

type DataTile struct {
	GID GID `xml:"gid,attr"`
}

func (p *Polygon) Decode() ([]Point, error) {
	return decodePoints(p.Points)
}
func (p *PolyLine) Decode() ([]Point, error) {
	return decodePoints(p.Points)
}

func decodePoints(s string) (points []Point, err error) {
	pointStrings := strings.Split(s, " ")

	points = make([]Point, len(pointStrings))
	for i, pointString := range pointStrings {
		coordStrings := strings.Split(pointString, ",")
		if len(coordStrings) != 2 {
			return []Point{}, InvalidPointsField
		}

		points[i].X, err = strconv.Atoi(coordStrings[0])
		if err != nil {
			return []Point{}, err
		}

		points[i].Y, err = strconv.Atoi(coordStrings[1])
		if err != nil {
			return []Point{}, err
		}
	}
	return
}

func getTileset(m *Map, l *Layer) (tileset *Tileset, isEmpty, usesMultipleTilesets bool) {
	for i := 0; i < len(l.DecodedTiles); i++ {
		tile := l.DecodedTiles[i]
		if !tile.Nil {
			if tileset == nil {
				tileset = tile.Tileset
			} else if tileset != tile.Tileset {
				return tileset, false, true
			}
		}
	}

	if tileset == nil {
		return nil, true, false
	}

	return tileset, false, false
}

func Read(r io.Reader) (*Map, error) {
	d := xml.NewDecoder(r)

	m := new(Map)
	if err := d.Decode(m); err != nil {
		return nil, err
	}

	err := m.decodeLayers()
	if err != nil {
		return nil, err
	}

	for i := 0; i < len(m.Layers); i++ {
		l := &m.Layers[i]

		tileset, isEmpty, usesMultipleTilesets := getTileset(m, l)
		if usesMultipleTilesets {
			continue
		}
		l.Empty, l.Tileset = isEmpty, tileset
	}

	return m, nil
}

func ReadFile(filePath string) (*Map, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}

	defer f.Close()

	newMap, err := Read(f)
	if err != nil {
		return nil, err
	}

	return newMap, err

}

func (m *Map) DecodeGID(gid GID) (*DecodedTile, error) {
	if gid == 0 {
		return NilTile, nil
	}

	gidBare := gid &^ GIDFlip

	for i := len(m.Tilesets) - 1; i >= 0; i-- {
		if m.Tilesets[i].FirstGID <= gidBare {
			return &DecodedTile{
				ID:             ID(gidBare - m.Tilesets[i].FirstGID),
				Tileset:        &m.Tilesets[i],
				HorizontalFlip: gid&GIDHorizontalFlip != 0,
				VerticalFlip:   gid&GIDVerticalFlip != 0,
				DiagonalFlip:   gid&GIDDiagonalFlip != 0,
				Nil:            false,
			}, nil
		}
	}

	return nil, InvalidGID // Should never hapen for a valid TMX file.
}

type DecodedTile struct {
	ID             ID
	Tileset        *Tileset
	HorizontalFlip bool
	VerticalFlip   bool
	DiagonalFlip   bool
	Nil            bool
}

func (t *DecodedTile) IsNil() bool {
	return t.Nil
}

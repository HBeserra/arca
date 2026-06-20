package catalogdb

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"stitchvault/internal/business/catalog"
	"stitchvault/internal/embroidery"
)

// dbDesign mirrors a designs row for scanning. JSON columns scan into `any`
// (DuckDB returns them already-decoded) and nullable Phase-2 columns into the
// sql.Null* types.
type dbDesign struct {
	ID              string
	Path            string
	FileName        string
	Format          string
	WidthMM         float64
	HeightMM        float64
	StitchCount     int
	ColorChanges    int
	ColorCount      int
	Palette         any
	ThumbnailPath   string
	FileSize        int64
	CreatedAt       time.Time
	Caption         sql.NullString
	Tags            any
	Style           sql.NullString
	VirtualFolderID sql.NullString
}

// dbDesignInsert holds the already-serialized values for an INSERT. Palette/Tags
// are JSON strings; Caption/Style/VirtualFolderID are nil for SQL NULL.
type dbDesignInsert struct {
	ID              string
	Path            string
	FileName        string
	Format          string
	WidthMM         float64
	HeightMM        float64
	StitchCount     int
	ColorChanges    int
	ColorCount      int
	Palette         string
	ThumbnailPath   string
	FileSize        int64
	CreatedAt       time.Time
	Caption         any
	Tags            string
	Style           any
	VirtualFolderID any
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanDesign(sc rowScanner) (dbDesign, error) {
	var m dbDesign
	err := sc.Scan(
		&m.ID, &m.Path, &m.FileName, &m.Format, &m.WidthMM, &m.HeightMM,
		&m.StitchCount, &m.ColorChanges, &m.ColorCount, &m.Palette, &m.ThumbnailPath,
		&m.FileSize, &m.CreatedAt, &m.Caption, &m.Tags, &m.Style, &m.VirtualFolderID,
	)
	if err != nil {
		return dbDesign{}, fmt.Errorf("scan design: %w", err)
	}
	return m, nil
}

func toDBDesign(d catalog.Design) (dbDesignInsert, error) {
	palette := d.Palette
	if palette == nil {
		palette = []embroidery.Thread{}
	}
	pb, err := json.Marshal(palette)
	if err != nil {
		return dbDesignInsert{}, fmt.Errorf("marshal palette: %w", err)
	}

	tags := d.Tags
	if tags == nil {
		tags = []string{}
	}
	tb, err := json.Marshal(tags)
	if err != nil {
		return dbDesignInsert{}, fmt.Errorf("marshal tags: %w", err)
	}

	createdAt := d.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}

	var caption, style, vfid any
	if d.Caption != "" {
		caption = d.Caption
	}
	if d.Style != "" {
		style = d.Style
	}
	if d.VirtualFolderID != nil {
		vfid = d.VirtualFolderID.String()
	}

	return dbDesignInsert{
		ID:              d.ID.String(),
		Path:            d.Path,
		FileName:        d.FileName,
		Format:          d.Format,
		WidthMM:         d.WidthMM,
		HeightMM:        d.HeightMM,
		StitchCount:     d.StitchCount,
		ColorChanges:    d.ColorChanges,
		ColorCount:      d.ColorCount,
		Palette:         string(pb),
		ThumbnailPath:   d.ThumbnailPath,
		FileSize:        d.FileSize,
		CreatedAt:       createdAt,
		Caption:         caption,
		Tags:            string(tb),
		Style:           style,
		VirtualFolderID: vfid,
	}, nil
}

func toDesign(m dbDesign) (catalog.Design, error) {
	id, err := uuid.Parse(m.ID)
	if err != nil {
		return catalog.Design{}, fmt.Errorf("parse design id: %w", err)
	}

	var palette []embroidery.Thread
	if err := decodeJSON(m.Palette, &palette); err != nil {
		return catalog.Design{}, fmt.Errorf("decode palette: %w", err)
	}

	var tags []string
	if err := decodeJSON(m.Tags, &tags); err != nil {
		return catalog.Design{}, fmt.Errorf("decode tags: %w", err)
	}

	var vfid *uuid.UUID
	if m.VirtualFolderID.Valid && m.VirtualFolderID.String != "" {
		p, err := uuid.Parse(m.VirtualFolderID.String)
		if err != nil {
			return catalog.Design{}, fmt.Errorf("parse virtual_folder_id: %w", err)
		}
		vfid = &p
	}

	return catalog.Design{
		ID:              id,
		Path:            m.Path,
		FileName:        m.FileName,
		Format:          m.Format,
		WidthMM:         m.WidthMM,
		HeightMM:        m.HeightMM,
		StitchCount:     m.StitchCount,
		ColorChanges:    m.ColorChanges,
		ColorCount:      m.ColorCount,
		Palette:         palette,
		ThumbnailPath:   m.ThumbnailPath,
		FileSize:        m.FileSize,
		CreatedAt:       m.CreatedAt,
		Caption:         m.Caption.String,
		Tags:            tags,
		Style:           m.Style.String,
		VirtualFolderID: vfid,
	}, nil
}

// decodeJSON unmarshals a DuckDB JSON column into dst, tolerating the driver
// returning it as a raw string, raw bytes, or an already-decoded Go value.
func decodeJSON(v any, dst any) error {
	switch t := v.(type) {
	case nil:
		return nil
	case string:
		if t == "" {
			return nil
		}
		return json.Unmarshal([]byte(t), dst)
	case []byte:
		if len(t) == 0 {
			return nil
		}
		return json.Unmarshal(t, dst)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		return json.Unmarshal(b, dst)
	}
}

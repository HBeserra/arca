// Package catalogdb implements catalog.Store on DuckDB. It opens its own tables in
// the SAME database file the RAG store (indexdb) uses, but is deliberately a
// separate store: the catalog is a flat, deterministic collection, and keeping it
// out of indexdb avoids its embedding-dimension reset logic ever touching catalog
// data.
package catalogdb

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"stitchvault/internal/business/catalog"
)

// Store is the DuckDB-backed catalog store.
type Store struct {
	log *slog.Logger
	db  *sql.DB
}

var _ catalog.Store = (*Store)(nil)

// New creates the store and initialises the catalog schema in db.
func New(log *slog.Logger, db *sql.DB) (*Store, error) {
	s := &Store{log: log, db: db}
	if err := s.init(); err != nil {
		return nil, fmt.Errorf("catalogdb init: %w", err)
	}
	return s, nil
}

func (s *Store) init() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS designs (
			id                TEXT      PRIMARY KEY,
			path              VARCHAR   NOT NULL,
			file_name         VARCHAR   NOT NULL,
			format            VARCHAR   NOT NULL,
			width_mm          DOUBLE    NOT NULL DEFAULT 0,
			height_mm         DOUBLE    NOT NULL DEFAULT 0,
			stitch_count      INTEGER   NOT NULL DEFAULT 0,
			color_changes     INTEGER   NOT NULL DEFAULT 0,
			color_count       INTEGER   NOT NULL DEFAULT 0,
			palette           JSON      NOT NULL DEFAULT '[]',
			thumbnail_path    VARCHAR   NOT NULL DEFAULT '',
			file_size_bytes   BIGINT    NOT NULL DEFAULT 0,
			created_at        TIMESTAMP NOT NULL DEFAULT current_timestamp,
			-- Phase 2 (nullable, written later by vision + clustering):
			caption           VARCHAR,
			tags              JSON      DEFAULT '[]',
			style             VARCHAR,
			embedding         FLOAT[768],
			virtual_folder_id TEXT
		);`,
		// Self-referential tree, mirrors documents.parent_id so the existing
		// DocTree frontend component renders it unchanged. Populated in Phase 2.
		`CREATE TABLE IF NOT EXISTS virtual_folders (
			id         TEXT      PRIMARY KEY,
			parent_id  TEXT,
			name       VARCHAR   NOT NULL,
			kind       VARCHAR   NOT NULL DEFAULT 'manual',
			rule       JSON,
			created_at TIMESTAMP NOT NULL DEFAULT current_timestamp
		);`,
	}

	for _, stmt := range stmts {
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("exec %q: %w", stmt[:min(40, len(stmt))], err)
		}
	}

	// Vector distance functions (array_cosine_similarity) come from the VSS
	// extension. Best-effort: semantic search degrades gracefully without it.
	if _, err := s.db.Exec(`INSTALL vss; LOAD vss;`); err != nil {
		s.log.Warn("catalogdb: VSS extension unavailable; semantic search disabled", "err", err)
	}
	return nil
}

// InsertDesign upserts a design by id (INSERT OR REPLACE), so re-importing the
// same file refreshes its metadata and thumbnail instead of duplicating it.
func (s *Store) InsertDesign(ctx context.Context, d catalog.Design) error {
	m, err := toDBDesign(d)
	if err != nil {
		return fmt.Errorf("insert design: %w", err)
	}

	_, err = s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO designs
			(id, path, file_name, format, width_mm, height_mm, stitch_count,
			 color_changes, color_count, palette, thumbnail_path, file_size_bytes,
			 created_at, caption, tags, style, virtual_folder_id)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`,
		m.ID, m.Path, m.FileName, m.Format, m.WidthMM, m.HeightMM, m.StitchCount,
		m.ColorChanges, m.ColorCount, m.Palette, m.ThumbnailPath, m.FileSize,
		m.CreatedAt, m.Caption, m.Tags, m.Style, m.VirtualFolderID,
	)
	if err != nil {
		return fmt.Errorf("insert design: %w", err)
	}
	return nil
}

const designColumns = `id, path, file_name, format, width_mm, height_mm,
	stitch_count, color_changes, color_count, palette, thumbnail_path,
	file_size_bytes, created_at, caption, tags, style, virtual_folder_id`

func (s *Store) ListDesigns(ctx context.Context, f catalog.Filter) ([]catalog.Design, error) {
	where, args := buildWhere(f)
	q := `SELECT ` + designColumns + ` FROM designs` + where +
		` ORDER BY created_at DESC, file_name`
	if f.Limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", f.Limit)
	}
	if f.Offset > 0 {
		q += fmt.Sprintf(" OFFSET %d", f.Offset)
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("list designs: %w", err)
	}
	defer rows.Close()

	var out []catalog.Design
	for rows.Next() {
		m, err := scanDesign(rows)
		if err != nil {
			return nil, err
		}
		d, err := toDesign(m)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list designs rows: %w", err)
	}
	return out, nil
}

func (s *Store) CountDesigns(ctx context.Context, f catalog.Filter) (int, error) {
	where, args := buildWhere(f)
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM designs`+where, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("count designs: %w", err)
	}
	return n, nil
}

func (s *Store) GetDesign(ctx context.Context, id uuid.UUID) (catalog.Design, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+designColumns+` FROM designs WHERE id = $1`, id.String())
	m, err := scanDesign(row)
	if err != nil {
		return catalog.Design{}, fmt.Errorf("get design: %w", err)
	}
	return toDesign(m)
}

func (s *Store) Facets(ctx context.Context) (catalog.Facets, error) {
	var fc catalog.Facets

	// Aggregate bounds + total in one pass.
	err := s.db.QueryRowContext(ctx, `
		SELECT COUNT(*),
		       COALESCE(MAX(GREATEST(width_mm, height_mm)), 0),
		       COALESCE(MAX(stitch_count), 0),
		       COALESCE(MAX(color_count), 0)
		FROM designs`).
		Scan(&fc.Total, &fc.MaxSizeMM, &fc.MaxStitches, &fc.MaxColors)
	if err != nil {
		return catalog.Facets{}, fmt.Errorf("facets aggregate: %w", err)
	}

	rows, err := s.db.QueryContext(ctx,
		`SELECT format, COUNT(*) FROM designs GROUP BY format ORDER BY format`)
	if err != nil {
		return catalog.Facets{}, fmt.Errorf("facets formats: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var c catalog.FacetCount
		if err := rows.Scan(&c.Value, &c.Count); err != nil {
			return catalog.Facets{}, fmt.Errorf("scan facet: %w", err)
		}
		fc.Formats = append(fc.Formats, c)
	}
	if err := rows.Err(); err != nil {
		return catalog.Facets{}, fmt.Errorf("facets rows: %w", err)
	}
	return fc, nil
}

// UpdateClassification stores the Phase-2 semantic attributes and the caption
// embedding for a design. A nil/empty embedding leaves that column untouched-NULL.
func (s *Store) UpdateClassification(ctx context.Context, id uuid.UUID, caption string, tags []string, style string, embedding []float32) error {
	if tags == nil {
		tags = []string{}
	}
	tb, err := json.Marshal(tags)
	if err != nil {
		return fmt.Errorf("update classification: marshal tags: %w", err)
	}

	var captionV, styleV any
	if caption != "" {
		captionV = caption
	}
	if style != "" {
		styleV = style
	}

	if len(embedding) == 0 {
		_, err = s.db.ExecContext(ctx,
			`UPDATE designs SET caption=$2, tags=$3, style=$4 WHERE id=$1`,
			id.String(), captionV, string(tb), styleV)
	} else {
		// FLOAT[] cannot be bound as a parameter, so interpolate it as a literal.
		q := fmt.Sprintf(
			`UPDATE designs SET caption=$2, tags=$3, style=$4, embedding=%s::FLOAT[%d] WHERE id=$1`,
			floatSliceToLiteral(embedding), len(embedding))
		_, err = s.db.ExecContext(ctx, q, id.String(), captionV, string(tb), styleV)
	}
	if err != nil {
		return fmt.Errorf("update classification: %w", err)
	}
	return nil
}

// SearchSimilar ranks designs that have an embedding by cosine similarity to
// queryVec, applying the same structured filters as ListDesigns. Exact search
// (no ANN index) — fast and precise at a personal-library scale.
func (s *Store) SearchSimilar(ctx context.Context, queryVec []float32, f catalog.Filter) ([]catalog.Design, error) {
	if len(queryVec) == 0 {
		return nil, nil
	}

	where, args := buildWhere(f)
	if where == "" {
		where = " WHERE embedding IS NOT NULL"
	} else {
		where += " AND embedding IS NOT NULL"
	}

	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}

	q := fmt.Sprintf(
		`SELECT %s FROM designs%s
		 ORDER BY array_cosine_similarity(embedding, %s::FLOAT[%d]) DESC
		 LIMIT %d`,
		designColumns, where, floatSliceToLiteral(queryVec), len(queryVec), limit)

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("search similar: %w", err)
	}
	defer rows.Close()

	var out []catalog.Design
	for rows.Next() {
		m, err := scanDesign(rows)
		if err != nil {
			return nil, err
		}
		d, err := toDesign(m)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search similar rows: %w", err)
	}
	return out, nil
}

func floatSliceToLiteral(vec []float32) string {
	var sb strings.Builder
	sb.WriteByte('[')
	for i, v := range vec {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(strconv.FormatFloat(float64(v), 'g', -1, 32))
	}
	sb.WriteByte(']')
	return sb.String()
}

// buildWhere turns a Filter into a parameterized WHERE clause (with a leading
// " WHERE " when non-empty) and its positional args.
func buildWhere(f catalog.Filter) (string, []any) {
	var conds []string
	var args []any

	addCmp := func(tmpl string, val any) {
		args = append(args, val)
		conds = append(conds, fmt.Sprintf(tmpl, len(args)))
	}

	if len(f.Formats) > 0 {
		ph := make([]string, len(f.Formats))
		for i, ft := range f.Formats {
			args = append(args, ft)
			ph[i] = fmt.Sprintf("$%d", len(args))
		}
		conds = append(conds, "format IN ("+strings.Join(ph, ",")+")")
	}
	if f.MinSizeMM > 0 {
		addCmp("GREATEST(width_mm, height_mm) >= $%d", f.MinSizeMM)
	}
	if f.MaxSizeMM > 0 {
		addCmp("GREATEST(width_mm, height_mm) <= $%d", f.MaxSizeMM)
	}
	if f.MinStitches > 0 {
		addCmp("stitch_count >= $%d", f.MinStitches)
	}
	if f.MaxStitches > 0 {
		addCmp("stitch_count <= $%d", f.MaxStitches)
	}
	if f.MinColors > 0 {
		addCmp("color_count >= $%d", f.MinColors)
	}
	if f.MaxColors > 0 {
		addCmp("color_count <= $%d", f.MaxColors)
	}
	if s := strings.TrimSpace(f.Search); s != "" {
		args = append(args, "%"+strings.ToLower(s)+"%")
		conds = append(conds, fmt.Sprintf("lower(file_name) LIKE $%d", len(args)))
	}

	if len(conds) == 0 {
		return "", nil
	}
	return " WHERE " + strings.Join(conds, " AND "), args
}

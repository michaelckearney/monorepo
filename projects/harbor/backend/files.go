package backend

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/engine"
)

// files keeps the engine's files in the database.
type files struct{ db *sql.DB }

func (f files) SaveFile(file engine.File) (engine.FileRef, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return engine.FileRef{}, err
	}
	id := hex.EncodeToString(raw)
	if file.Type == "" {
		file.Type = "application/octet-stream"
	}
	_, err := f.db.Exec(`INSERT INTO files (id, name, type, size, data) VALUES (?, ?, ?, ?, ?)`,
		id, file.Name, file.Type, len(file.Data), file.Data)
	return engine.FileRef{File: id, Name: file.Name, Type: file.Type, Size: len(file.Data)}, err
}

func (f files) File(id string) (engine.File, error) {
	var file engine.File
	err := f.db.QueryRow(`SELECT name, type, data FROM files WHERE id = ?`, id).Scan(&file.Name, &file.Type, &file.Data)
	if errors.Is(err, sql.ErrNoRows) {
		return file, fmt.Errorf("there is no file %s", id)
	}
	return file, err
}

// FileData is a file, for the window: its bytes in base64, as the bindings
// carry them.
type FileData struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Data string `json:"data"`
}

// File reads a file a tool returned, to show it or save it.
func (t *Tools) File(id string) (FileData, error) {
	f, err := files{t.db}.File(id)
	if err != nil {
		return FileData{}, err
	}
	return FileData{Name: f.Name, Type: f.Type, Data: base64.StdEncoding.EncodeToString(f.Data)}, nil
}

// AddFile keeps a file you chose, for a tool's input, and returns what
// names it there.
func (t *Tools) AddFile(f FileData) (engine.FileRef, error) {
	data, err := base64.StdEncoding.DecodeString(f.Data)
	if err != nil {
		return engine.FileRef{}, fmt.Errorf("the file's bytes are not base64: %w", err)
	}
	return files{t.db}.SaveFile(engine.File{Name: f.Name, Type: f.Type, Data: data})
}

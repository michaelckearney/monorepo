package backend

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/michaelckearney/monorepo/projects/harbor/backend/service"
)

// readManifests reads the manifest of every service that has none: one a
// migration inserted, such as the dev fixtures, or one whose manifest a
// migration cleared. It runs as Harbor starts, before anything reads them. A
// source that will not read keeps no manifest, and the error says why, so
// one broken service does not stop Harbor.
func readManifests(db *sql.DB) error {
	rows, err := db.Query(`SELECT id, name, source FROM services WHERE manifest IS NULL`)
	if err != nil {
		return err
	}
	type unread struct {
		id           int64
		name, source string
	}
	var todo []unread
	for rows.Next() {
		var u unread
		if err := rows.Scan(&u.id, &u.name, &u.source); err != nil {
			rows.Close()
			return err
		}
		todo = append(todo, u)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, u := range todo {
		m, readErr := readManifest(u.source)
		if readErr == nil && m.Name != u.name {
			readErr = fmt.Errorf("the file names the service %s, but it is saved as %s", m.Name, u.name)
		}
		if readErr != nil {
			if _, err := db.Exec(`UPDATE services SET manifest_error = ? WHERE id = ?`, readErr.Error(), u.id); err != nil {
				return err
			}
			continue
		}
		if _, err := db.Exec(`UPDATE services SET manifest = ?, manifest_error = NULL WHERE id = ?`, m.JSON, u.id); err != nil {
			return err
		}
	}
	return nil
}

// manifest is a service's manifest, and the JSON Harbor stores it as.
type manifest struct {
	service.Manifest
	JSON string
}

func readManifest(source string) (manifest, error) {
	m, err := service.Read(source)
	if err != nil {
		return manifest{}, err
	}
	data, err := json.Marshal(m)
	if err != nil {
		return manifest{}, err
	}
	// As text: bytes would be stored as a BLOB, which reaches the frontend
	// as base64.
	return manifest{Manifest: m, JSON: string(data)}, nil
}

package backend

import (
	"database/sql"
	"encoding/base64"
	"errors"

	"github.com/michaelckearney/monorepo/framework/browser"
	"github.com/michaelckearney/monorepo/framework/browser/passkey"
	"github.com/michaelckearney/monorepo/projects/harbor/backend/engine"
)

// secrets keeps the engine's passwords and passkeys in the database.
type secrets struct{ db *sql.DB }

func (s secrets) Password(origin, realm string) (browser.Credentials, bool, error) {
	var c browser.Credentials
	err := s.db.QueryRow(`SELECT username, password FROM http_passwords WHERE origin = ? AND realm = ?`, origin, realm).
		Scan(&c.Username, &c.Password)
	if errors.Is(err, sql.ErrNoRows) {
		return c, false, nil
	}
	return c, err == nil, err
}

func (s secrets) SavePassword(origin, realm string, c browser.Credentials) error {
	_, err := s.db.Exec(`INSERT INTO http_passwords (origin, realm, username, password) VALUES (?, ?, ?, ?)
		ON CONFLICT (origin, realm) DO UPDATE SET username = excluded.username, password = excluded.password`,
		origin, realm, c.Username, c.Password)
	return err
}

var b64url = base64.RawURLEncoding

func (s secrets) Passkeys(rpID string) ([]engine.Passkey, error) {
	rows, err := s.db.Query(`SELECT id, user_id, user_name, user_display_name, private_key, auto
		FROM passkeys WHERE rp_id = ? ORDER BY created`, rpID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []engine.Passkey
	for rows.Next() {
		var id, userID, private string
		k := engine.Passkey{Key: passkey.Key{RPID: rpID}}
		if err := rows.Scan(&id, &userID, &k.UserName, &k.UserDisplayName, &private, &k.Auto); err != nil {
			return nil, err
		}
		if k.ID, err = b64url.DecodeString(id); err != nil {
			return nil, err
		}
		if k.UserID, err = b64url.DecodeString(userID); err != nil {
			return nil, err
		}
		if k.Private, err = base64.StdEncoding.DecodeString(private); err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	return keys, rows.Err()
}

func (s secrets) SavePasskey(k passkey.Key) error {
	_, err := s.db.Exec(`INSERT INTO passkeys (id, rp_id, user_id, user_name, user_display_name, private_key)
		VALUES (?, ?, ?, ?, ?, ?)`,
		b64url.EncodeToString(k.ID), k.RPID, b64url.EncodeToString(k.UserID), k.UserName, k.UserDisplayName,
		base64.StdEncoding.EncodeToString(k.Private))
	return err
}

func (s secrets) Login(origin string) (engine.Login, bool, error) {
	var l engine.Login
	err := s.db.QueryRow(`SELECT username, password, totp_secret FROM logins WHERE origin = ?`, origin).
		Scan(&l.Username, &l.Password, &l.TOTPSecret)
	if errors.Is(err, sql.ErrNoRows) {
		return l, false, nil
	}
	return l, err == nil, err
}

func (s secrets) SaveLogin(origin string, l engine.Login) error {
	_, err := s.db.Exec(`INSERT INTO logins (origin, username, password, totp_secret) VALUES (?, ?, ?, ?)
		ON CONFLICT (origin) DO UPDATE SET username = excluded.username, password = excluded.password,
			totp_secret = excluded.totp_secret`,
		origin, l.Username, l.Password, l.TOTPSecret)
	return err
}

func (s secrets) UsePasskeyWithoutAsking(id []byte, auto bool) error {
	_, err := s.db.Exec(`UPDATE passkeys SET auto = ? WHERE id = ?`, auto, b64url.EncodeToString(id))
	return err
}

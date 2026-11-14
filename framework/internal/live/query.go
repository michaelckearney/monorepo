package live

import (
	"bytes"
	"encoding/json"
)

// depend records which tables s reads. EXPLAIN lists the b-trees a statement
// opens by root page; sqlite_schema maps a root page to its table, and an
// index's to the table it indexes. This sees through joins, views, and
// subqueries. A query that opens something it cannot name, such as a virtual
// table, is rerun on every change instead.
func (e *Engine) depend(s *sub) {
	tables, all := e.tablesRead(s.query, s.args)
	e.mu.Lock()
	s.tables, s.all = tables, all
	e.mu.Unlock()
}

func (e *Engine) tablesRead(query string, args []any) (map[string]bool, bool) {
	rows, err := e.reader.Query("EXPLAIN "+query, args...)
	if err != nil {
		// The query itself will fail and report why; until it is fixed, any
		// change might be the one that fixes it.
		return nil, true
	}
	defer rows.Close()
	var pages []int64
	for rows.Next() {
		var addr, p1, p2, p3, p5 int64
		var opcode string
		var p4, comment any
		if err := rows.Scan(&addr, &opcode, &p1, &p2, &p3, &p4, &p5, &comment); err != nil {
			return nil, true
		}
		switch opcode {
		case "OpenRead", "ReopenIdx":
			if p3 != 0 { // a database other than main, such as temp
				return nil, true
			}
			pages = append(pages, p2)
		case "VOpen":
			return nil, true
		}
	}
	if rows.Err() != nil {
		return nil, true
	}

	byPage := map[int64]string{}
	schema, err := e.reader.Query("SELECT rootpage, tbl_name FROM sqlite_schema WHERE rootpage > 0")
	if err != nil {
		return nil, true
	}
	defer schema.Close()
	for schema.Next() {
		var page int64
		var table string
		if err := schema.Scan(&page, &table); err != nil {
			return nil, true
		}
		byPage[page] = table
	}
	tables := map[string]bool{}
	for _, p := range pages {
		t, ok := byPage[p]
		if !ok {
			return nil, true
		}
		tables[t] = true
	}
	return tables, false
}

// run runs a query and returns each row as a JSON object, keyed by column
// name in the order the query gives them.
func (e *Engine) run(query string, args []any) ([]string, error) {
	rows, err := e.reader.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	keys := make([][]byte, len(cols))
	for i, c := range cols {
		k, _ := json.Marshal(c)
		keys[i] = k
	}
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	out := []string{}
	var b bytes.Buffer
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		b.Reset()
		b.WriteByte('{')
		for i, v := range vals {
			if i > 0 {
				b.WriteByte(',')
			}
			b.Write(keys[i])
			b.WriteByte(':')
			enc, err := json.Marshal(v)
			if err != nil {
				return nil, err
			}
			b.Write(enc)
		}
		b.WriteByte('}')
		out = append(out, b.String())
	}
	return out, rows.Err()
}

// diff finds the one splice that turns old into new: the rows between the
// longest common prefix and the longest common suffix. An append, an insert,
// a delete, or an edit of one row is exactly that row. Changes far apart send
// everything between them, which is still correct.
func diff(old, new []string) (start, del int, ins []string, changed bool) {
	for start < len(old) && start < len(new) && old[start] == new[start] {
		start++
	}
	oldEnd, newEnd := len(old), len(new)
	for oldEnd > start && newEnd > start && old[oldEnd-1] == new[newEnd-1] {
		oldEnd--
		newEnd--
	}
	if oldEnd == start && newEnd == start {
		return 0, 0, nil, false
	}
	return start, oldEnd - start, new[start:newEnd], true
}

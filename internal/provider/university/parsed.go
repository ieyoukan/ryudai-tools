package university

import "crypto/sha256"

type parsedKey struct {
	Hash        [32]byte
	Title, Kind string
}

type parsedDocument struct {
	Rows  []TimetableCourse
	Pages []string
	Err   error
}

// Cache immutable parsed public documents by content, independently of fetched
// metadata. Refreshes and stale fallbacks always receive the current Source.
func (c *Client) parsedDocument(data []byte, doc Document, kind string) parsedDocument {
	key := parsedKey{Hash: sha256.Sum256(data), Title: doc.Title, Kind: kind}
	c.parseMu.Lock()
	defer c.parseMu.Unlock()
	if p, ok := c.parsed[key]; ok {
		return p
	}
	var p parsedDocument
	if kind == "timetable" {
		p.Rows, p.Err = ParseTimetablePDF(data, doc, Source{})
	} else {
		p.Pages, p.Err = pdfPages(data)
	}
	if c.parsed == nil || len(c.parsed) >= 64 {
		c.parsed = make(map[parsedKey]parsedDocument)
	}
	c.parsed[key] = p
	return p
}

func (c *Client) timetableRows(data []byte, doc Document, src Source) ([]TimetableCourse, error) {
	p := c.parsedDocument(data, doc, "timetable")
	if p.Err != nil {
		return nil, p.Err
	}
	rows := append([]TimetableCourse(nil), p.Rows...)
	for i := range rows {
		rows[i].Source = src
		if rows[i].Credits != nil {
			n := *rows[i].Credits
			rows[i].Credits = &n
		}
		if rows[i].Capacity != nil {
			n := *rows[i].Capacity
			rows[i].Capacity = &n
		}
	}
	return rows, nil
}

func (c *Client) documentPages(data []byte, doc Document) ([]string, error) {
	p := c.parsedDocument(data, doc, "pages")
	return append([]string(nil), p.Pages...), p.Err
}

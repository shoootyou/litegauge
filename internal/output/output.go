// Package output prints results. Every byte goes through the methods below, which take typed records
// only, so no text from a gateway response has a path to the screen.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"

	"github.com/shoootyou/litegauge/internal/keys"
	"github.com/shoootyou/litegauge/internal/messages"
	"github.com/shoootyou/litegauge/internal/verdict"
)

// Printer writes one result in Format (table, json or yaml). Fancy is the bordered, coloured table.
type Printer struct {
	W      io.Writer
	Format string
	Fancy  bool
}

type kv struct {
	key string
	val any // a string or an int
}

type record []kv

// document is the one shape of json and yaml output: a named list of flat records, plus the key names for a matrix.
type document struct {
	name    string
	records []record
	keys    []string
}

func jsonOf(v any) string {
	b, _ := json.Marshal(v) // a string or an int always marshals
	return string(b)
}

// json writes one compact line. The "keys" list is joined with "," like every other list.
func (d document) json() string {
	recs := make([]string, len(d.records))
	for i, r := range d.records {
		fields := make([]string, len(r))
		for j, f := range r {
			fields[j] = jsonOf(f.key) + ":" + jsonOf(f.val)
		}
		recs[i] = "{" + strings.Join(fields, ",") + "}"
	}
	s := `{"` + d.name + `":[` + strings.Join(recs, ",") + `]`
	if d.keys != nil {
		names := make([]string, len(d.keys))
		for i, k := range d.keys {
			names[i] = jsonOf(k)
		}
		s += `,"keys":[` + strings.Join(names, ",") + `]`
	}
	return s + "}\n"
}

// yaml writes block style. Every string goes through json.Marshal: a JSON string is a valid YAML double-quoted scalar.
func (d document) yaml() string {
	var b strings.Builder
	if len(d.records) == 0 {
		b.WriteString(d.name + ": []\n")
	} else {
		b.WriteString(d.name + ":\n")
	}
	for _, r := range d.records {
		for j, f := range r {
			lead := "    "
			if j == 0 {
				lead = "  - "
			}
			b.WriteString(lead + f.key + ": " + jsonOf(f.val) + "\n")
		}
	}
	if d.keys != nil {
		b.WriteString("keys:\n")
		for _, k := range d.keys {
			b.WriteString("  - " + jsonOf(k) + "\n")
		}
	}
	return b.String()
}

// emit writes a document, or a table: styled output goes through lipgloss, which drops the colour
// when the writer is not a terminal.
func (p Printer) emit(d document, tableText func() string) {
	switch p.Format {
	case "json":
		_, _ = io.WriteString(p.W, d.json())
	case "yaml":
		_, _ = io.WriteString(p.W, d.yaml())
	case "table":
		if s := tableText(); p.Fancy {
			_, _ = lipgloss.Fprint(p.W, s)
		} else {
			_, _ = io.WriteString(p.W, s)
		}
	}
}

func verdictStyle(v verdict.Verdict) lipgloss.Style {
	s := lipgloss.NewStyle()
	switch v {
	case verdict.OK:
		return s.Foreground(lipgloss.Green)
	case verdict.KeyValid:
		return s.Faint(true)
	case verdict.Blocked:
		return s.Foreground(lipgloss.Yellow)
	case verdict.ModelNotFound, verdict.Error:
		return s.Foreground(lipgloss.BrightBlack)
	}
	return s.Foreground(lipgloss.Red)
}

// paint styles s only on the fancy output.
func (p Printer) paint(st lipgloss.Style, s string) string {
	if !p.Fancy {
		return s
	}
	return st.Render(s)
}

// groupTables draws one titled table per model group, in order of first appearance; row(i) is the row of model i.
func (p Printer) groupTables(models, head []string, row func(i int) []string) string {
	names, idx := groupIndex(models)
	blocks := make([]string, len(names))
	for n, g := range names {
		rows := make([][]string, len(idx[g]))
		for r, i := range idx[g] {
			rows[r] = row(i)
		}
		blocks[n] = p.paint(lipgloss.NewStyle().Bold(true), g) + "\n" + p.renderTable(head, rows)
	}
	return strings.Join(blocks, "\n")
}

// renderTable draws a rounded table with a bold header, or plain columns joined by two spaces without trailing spaces.
func (p Printer) renderTable(head []string, rows [][]string) string {
	if p.Fancy {
		t := table.New().Border(lipgloss.RoundedBorder()).Rows(rows...).StyleFunc(func(row, _ int) lipgloss.Style {
			s := lipgloss.NewStyle().Padding(0, 1)
			if row == table.HeaderRow {
				s = s.Bold(true)
			}
			return s
		})
		if head != nil {
			t.Headers(head...)
		}
		return t.String() + "\n"
	}
	all := rows
	if head != nil {
		all = append([][]string{head}, rows...)
	}
	widths := make([]int, len(all[0]))
	for _, r := range all {
		for i, c := range r {
			widths[i] = max(widths[i], lipgloss.Width(c))
		}
	}
	var b strings.Builder
	for _, r := range all {
		line := make([]string, len(r))
		for i, c := range r {
			line[i] = c + strings.Repeat(" ", widths[i]-lipgloss.Width(c))
		}
		b.WriteString(strings.TrimRight(strings.Join(line, "  "), " ") + "\n")
	}
	return b.String()
}

// groupOf names the table a model belongs to: the part before "/", else before the first "-".
func groupOf(id string) string {
	low := strings.ToLower(id)
	if first, _, ok := strings.Cut(low, "/"); ok {
		return first
	}
	first, _, _ := strings.Cut(low, "-")
	return first
}

// groupIndex lists the model positions per group, groups in order of first appearance.
func groupIndex(models []string) (names []string, idx map[string][]int) {
	idx = map[string][]int{}
	for i, id := range models {
		g := groupOf(id)
		if _, seen := idx[g]; !seen {
			names = append(names, g)
		}
		idx[g] = append(idx[g], i)
	}
	return names, idx
}

func resultRecord(r verdict.Result) record {
	return record{{"key", r.Key}, {"model", r.Model}, {"group", groupOf(r.Model)}, {"price", string(r.Price)},
		{"verdict", string(r.Verdict)}, {"http_status", r.Status}, {"latency_ms", r.LatencyMS}}
}

// detailOf is the Detail line; an error verdict names its cause by the HTTP status.
func detailOf(r verdict.Result) string {
	d := messages.ErrorDetail
	switch {
	case r.Verdict != verdict.Error:
		return messages.Verdicts[r.Verdict].Detail
	case r.Status == 0:
		return d.NoReply
	case r.Status >= 300 && r.Status < 400:
		return d.Redirect
	case r.Status == 429:
		return d.Rate
	case r.Status >= 500:
		return d.Server
	}
	return d.Other
}

func (p Printer) Probe(r verdict.Result) {
	d := document{name: "results", records: []record{resultRecord(r)}}
	p.emit(d, func() string {
		vt := messages.Verdicts[r.Verdict]
		l := messages.Labels
		latency := fmt.Sprintf(l.LatencyOnly, r.LatencyMS)
		if r.Status > 0 {
			latency = fmt.Sprintf(l.HTTPLatency, r.Status, r.LatencyMS)
		}
		return p.renderTable(nil, [][]string{
			{l.Key, r.Key},
			{l.Model, r.Model + " (" + messages.Prices[r.Price].Long + ")"},
			{l.Result, p.paint(verdictStyle(r.Verdict), vt.Symbol+" "+vt.Word) + "  " + latency},
			{l.Detail, detailOf(r)},
		})
	})
}

// Matrix prints cells[model][key]: one table per group, results model by model. A model's price is its cells' price.
func (p Printer) Matrix(ks []keys.Entry, models []string, cells [][]verdict.Result) {
	names, idx := groupIndex(models)
	d := document{name: "results", records: []record{}, keys: make([]string, len(ks))}
	for i, k := range ks {
		d.keys[i] = k.Name
	}
	for _, g := range names {
		for _, m := range idx[g] {
			for _, r := range cells[m] {
				d.records = append(d.records, resultRecord(r))
			}
		}
	}
	p.emit(d, func() string {
		head := append([]string{messages.Labels.Model, messages.Labels.Price}, d.keys...)
		return p.groupTables(models, head, func(m int) []string {
			row := []string{models[m], messages.Prices[cells[m][0].Price].Short}
			for _, r := range cells[m] {
				vt := messages.Verdicts[r.Verdict]
				row = append(row, p.paint(verdictStyle(r.Verdict), vt.Symbol+" "+vt.Cell))
			}
			return row
		}) + messages.Labels.Legend + "\n"
	})
}

// Models prints the ids with their prices, by position.
func (p Printer) Models(ids []string, prices []verdict.Price) {
	d := document{name: "models", records: []record{}}
	for i, id := range ids {
		d.records = append(d.records, record{{"model", id}, {"group", groupOf(id)}, {"price", string(prices[i])}})
	}
	p.emit(d, func() string {
		return p.groupTables(ids, []string{messages.Labels.Model, messages.Labels.Price}, func(i int) []string {
			return []string{ids[i], messages.Prices[prices[i]].Long}
		})
	})
}

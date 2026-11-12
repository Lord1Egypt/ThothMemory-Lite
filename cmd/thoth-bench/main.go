// Command thoth-bench produces the measurements in docs/PERFORMANCE.md. It is a
// development tool, not part of releases, and only touches the directory it
// is given.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Lord1Egypt/ThothMemory-Lite/internal/memory"
	"github.com/Lord1Egypt/ThothMemory-Lite/internal/sqlite"
)

var english = []string{"migration", "checkpoint", "rollback", "transaction", "handoff", "regression", "timeout",
	"deadlock", "schema", "index", "retry", "credential", "parser", "renderer", "scheduler", "fixture",
	"benchmark", "release", "upgrade", "compaction", "session", "evidence", "decision", "WAL", "fsync"}
var arabic = []string{"المشكلة", "اتحلت", "إعدادات", "الاتصال", "قاعدة", "البيانات", "اختبار", "فشل", "التحقق",
	"تعارض", "المراجعات", "النسخة", "الاحتياطية", "الترحيل", "الجلسة"}

func sentence(r *rand.Rand) string {
	n := 6 + r.IntN(14)
	w := make([]string, n)
	for i := range w {
		switch x := r.IntN(10); {
		case x < 6:
			w[i] = english[r.IntN(len(english))]
		case x < 9:
			w[i] = arabic[r.IntN(len(arabic))]
		default:
			w[i] = fmt.Sprintf("E%05d", r.IntN(99999))
		}
	}
	return strings.Join(w, " ")
}

func hex40(r *rand.Rand) string {
	const h = "0123456789abcdef"
	b := make([]byte, 40)
	for i := range b {
		b[i] = h[r.IntN(16)]
	}
	return string(b)
}

// seed writes projects*events events and events/10 records per project
// directly in large transactions; content mirrors what agents store.
func seed(ctx context.Context, s *sqlite.Store, projects, events int) (string, map[string]string, error) {
	r := rand.New(rand.NewPCG(1, 2))
	now := time.Now().UTC()
	ts := func(i int) string {
		return now.Add(time.Duration(i) * time.Millisecond).Format("2006-01-02T15:04:05.000000Z")
	}
	samples := map[string]string{}
	if _, e := s.DB.ExecContext(ctx, "INSERT OR IGNORE INTO principals(id,created_at) VALUES('bench',?)", ts(0)); e != nil {
		return "", nil, e
	}
	var target string
	for p := 0; p < projects; p++ {
		pid := memory.NewID()
		if p == 0 {
			target = pid
		}
		tx, e := s.DB.BeginTx(ctx, nil)
		if e != nil {
			return "", nil, e
		}
		if _, e = tx.ExecContext(ctx, "INSERT INTO projects(id,name,repository,created_at,event_budget) VALUES(?,?,?,?,10000000)", pid, fmt.Sprint("bench ", p), fmt.Sprintf("github.com/bench/p%d-%d", p, now.UnixNano()), ts(0)); e != nil {
			return "", nil, e
		}
		ev, _ := tx.PrepareContext(ctx, "INSERT INTO events(id,project_id,kind,body,principal_id,agent_id,git_revision,created_at,details,search_text) VALUES(?,?,?,?,'bench','codex',?,?,'{}',?)")
		rec, _ := tx.PrepareContext(ctx, "INSERT INTO records(id,project_id,kind,title,body,git_revision,principal_id,agent_id,created_at,search_text) VALUES(?,?,?,?,?,?,'bench','claude',?,?)")
		for i := 0; i < events; i++ {
			body, rev, id := sentence(r), hex40(r), memory.NewID()
			if _, e = ev.ExecContext(ctx, id, pid, "progress", body, rev, ts(i), memory.SearchText("progress", body, rev)); e != nil {
				return "", nil, e
			}
			if p == 0 && i == events/2 {
				samples["exact_id"], samples["commit_prefix"] = id, rev[:12]
			}
			if i%10 == 0 {
				title, rbody, rid := sentence(r), sentence(r)+" "+sentence(r), memory.NewID()
				kind := []string{"decision", "issue", "observation", "note"}[r.IntN(4)]
				if _, e = rec.ExecContext(ctx, rid, pid, kind, title[:min(len(title), 200)], rbody, rev, ts(i), memory.SearchText(title, rbody, rev)); e != nil {
					return "", nil, e
				}
			}
		}
		if e = tx.Commit(); e != nil {
			return "", nil, e
		}
	}
	return target, samples, nil
}

type result struct {
	Query   string  `json:"query"`
	Kind    string  `json:"kind"`
	Hits    int     `json:"hits"`
	P50ms   float64 `json:"p50_ms"`
	P95ms   float64 `json:"p95_ms"`
	Samples int     `json:"samples"`
}

func timeSearch(ctx context.Context, s *sqlite.Store, project, kind string, q memory.SearchQuery, n int) (result, error) {
	var lat []float64
	hits := 0
	for i := 0; i < n; i++ {
		t := time.Now()
		rows, e := s.Search(ctx, project, q)
		if e != nil {
			return result{}, e
		}
		lat = append(lat, float64(time.Since(t).Microseconds())/1000)
		hits = len(rows)
	}
	sort.Float64s(lat)
	return result{Query: q.Text + q.ExactID, Kind: kind, Hits: hits, P50ms: lat[len(lat)/2], P95ms: lat[len(lat)*95/100], Samples: n}, nil
}

func main() {
	dir := flag.String("dir", "", "empty scratch directory (required)")
	projects := flag.Int("projects", 3, "projects")
	events := flag.Int("events", 10000, "events per project")
	reps := flag.Int("reps", 20, "repetitions per query")
	mode := flag.String("mode", "search", "search | ops")
	writers := flag.Int("writers", 4, "concurrent writers (ops mode)")
	flag.Parse()
	if *dir == "" {
		fmt.Fprintln(os.Stderr, "thoth-bench -dir SCRATCH [-projects N] [-events N]")
		os.Exit(2)
	}
	ctx := context.Background()
	path := filepath.Join(*dir, "bench.db")
	if _, e := os.Stat(path); e == nil {
		fmt.Fprintln(os.Stderr, "refusing to reuse", path)
		os.Exit(2)
	}
	s, e := sqlite.Open(ctx, path)
	if e == nil {
		e = s.Migrate(ctx)
	}
	if e != nil {
		panic(e)
	}
	defer s.Close()
	if *mode == "ops" {
		out, e := ops(ctx, s, *dir, *reps, *writers)
		if e != nil {
			panic(e)
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
		return
	}
	t := time.Now()
	project, samples, e := seed(ctx, s, *projects, *events)
	if e != nil {
		panic(e)
	}
	seedTime := time.Since(t)
	st, _ := os.Stat(path)
	queries := []struct {
		kind string
		q    memory.SearchQuery
	}{
		{"exact id", memory.SearchQuery{ExactID: samples["exact_id"]}},
		{"commit prefix", memory.SearchQuery{Text: samples["commit_prefix"]}},
		{"english word", memory.SearchQuery{Text: "deadlock"}},
		{"english stem", memory.SearchQuery{Text: "migrations"}},
		{"arabic word", memory.SearchQuery{Text: "اتحلت"}},
		{"arabic substring", memory.SearchQuery{Text: "إعداد"}},
		{"mixed arabic/english", memory.SearchQuery{Text: "rollback الاتصال"}},
		{"short query", memory.SearchQuery{Text: "wa"}},
		{"symbol/code", memory.SearchQuery{Text: "E1234"}},
		{"no match", memory.SearchQuery{Text: "zzzzqqqq"}},
	}
	out := map[string]any{"projects": *projects, "events_per_project": *events, "records_per_project": *events / 10,
		"seed_seconds": seedTime.Seconds(), "database_bytes": st.Size()}
	var rs []result
	for _, x := range queries {
		x.q.Limit = 20
		r, e := timeSearch(ctx, s, project, x.kind, x.q, *reps)
		if e != nil {
			panic(e)
		}
		rs = append(rs, r)
	}
	out["search"] = rs
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	_ = enc.Encode(out)
}

func percentiles(lat []float64) map[string]float64 {
	sort.Float64s(lat)
	return map[string]float64{"p50_ms": lat[len(lat)/2], "p95_ms": lat[len(lat)*95/100], "max_ms": lat[len(lat)-1], "samples": float64(len(lat))}
}

// ops measures the MCP tool layer directly: durable writes (synchronous=FULL,
// one transaction and fsync each), briefing reads, concurrent writers and
// backup/restore throughput.
func ops(ctx context.Context, s *sqlite.Store, dir string, n, writers int) (map[string]any, error) {
	p := memory.Principal{ID: "bench", Write: true, Projects: []string{"*"}}
	if e := s.Register(ctx, p); e != nil {
		return nil, e
	}
	v, e := s.Call(ctx, p, "create_project", memory.Input{Name: "ops", Repository: "https://github.com/bench/ops"})
	if e != nil {
		return nil, e
	}
	project := v.(memory.Row)["id"].(string)
	timeIt := func(f func(i int) error) (map[string]float64, error) {
		var lat []float64
		for i := 0; i < n; i++ {
			t := time.Now()
			if e := f(i); e != nil {
				return nil, e
			}
			lat = append(lat, float64(time.Since(t).Microseconds())/1000)
		}
		return percentiles(lat), nil
	}
	out := map[string]any{"iterations": n}
	var err error
	if out["append_event"], err = timeIt(func(i int) error {
		_, e := s.Call(ctx, p, "append_event", memory.Input{ProjectID: project, OperationID: fmt.Sprintf("bench-event-%06d", i), Body: "benchmark progress event with some realistic length of text"})
		return e
	}); err != nil {
		return nil, err
	}
	if out["record_decision"], err = timeIt(func(i int) error {
		_, e := s.Call(ctx, p, "record_decision", memory.Input{ProjectID: project, OperationID: fmt.Sprintf("bench-dec-%06d", i), Title: fmt.Sprint("Decision ", i), Body: "Because of measured reasons."})
		return e
	}); err != nil {
		return nil, err
	}
	if out["get_project_context"], err = timeIt(func(int) error {
		_, e := s.Call(ctx, p, "get_project_context", memory.Input{ProjectID: project})
		return e
	}); err != nil {
		return nil, err
	}
	// Concurrent writers, each with its own connection pool, like separate agents.
	start := time.Now()
	errs := make(chan error, writers)
	for w := 0; w < writers; w++ {
		go func(w int) {
			st, e := sqlite.Open(ctx, filepath.Join(dir, "bench.db"))
			if e != nil {
				errs <- e
				return
			}
			defer st.Close()
			for i := 0; i < n; i++ {
				if _, e = st.Call(ctx, p, "append_event", memory.Input{ProjectID: project, OperationID: fmt.Sprintf("bench-w%02d-%06d", w, i), Body: "concurrent write"}); e != nil {
					errs <- e
					return
				}
			}
			errs <- nil
		}(w)
	}
	for w := 0; w < writers; w++ {
		if e := <-errs; e != nil {
			return nil, e
		}
	}
	el := time.Since(start).Seconds()
	out["concurrent_writers"] = map[string]float64{"writers": float64(writers), "writes": float64(writers * n), "seconds": el, "writes_per_second": float64(writers*n) / el}
	backup := filepath.Join(dir, "bench-backup.db")
	t := time.Now()
	if _, e := s.Backup(ctx, backup); e != nil {
		return nil, e
	}
	st, _ := os.Stat(backup)
	out["backup"] = map[string]float64{"bytes": float64(st.Size()), "seconds": time.Since(t).Seconds()}
	return out, nil
}

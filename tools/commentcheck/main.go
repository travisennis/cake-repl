package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

var directives = []string{
	"//go:",
	"// +build",
	"//line ",
	"//export ",
	"//extern ",
	"//nolint",
	"// #nosec",
	"//lint:",
	"//revive:",
	"// Code generated ",
}

type entry struct {
	count int
	path  string
	hash  string
}

type finding struct {
	path string
	line int
	text string
}

func directive(text string) bool {
	for _, prefix := range directives {
		if strings.HasPrefix(text, prefix) {
			return true
		}
	}
	return false
}

func normalize(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func key(path, text string) string {
	sum := sha256.Sum256([]byte(path + "\n" + text))
	return path + "\t" + hex.EncodeToString(sum[:])
}

func scanJustfile(path, rel string, counts map[string]entry, first map[string]finding) error {
	data, err := os.ReadFile(path) // #nosec G304 G122 -- path comes from the repository walk
	if err != nil {
		return err
	}
	for i, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "#!") {
			continue
		}
		text := normalize(trimmed)
		k := key(rel, text)
		counts[k] = entry{count: counts[k].count + 1, path: rel, hash: strings.Split(k, "\t")[1]}
		if _, ok := first[k]; !ok {
			first[k] = finding{path: rel, line: i + 1, text: text}
		}
	}
	return nil
}

func scan(root string) (map[string]entry, map[string]finding, error) {
	counts := make(map[string]entry)
	first := make(map[string]finding)
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "bin", "dist", "vendor":
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == "justfile" {
			relPath, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			return scanJustfile(path, relPath, counts, first)
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		src, err := os.ReadFile(path) // #nosec G304 G122 -- path comes from the repository walk
		if err != nil {
			return err
		}
		f, err := parser.ParseFile(fset, path, src, parser.ParseComments)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		for _, group := range f.Comments {
			allowed := true
			lines := make([]string, 0, len(group.List))
			for _, c := range group.List {
				if !directive(c.Text) {
					allowed = false
				}
				lines = append(lines, c.Text)
			}
			if allowed {
				continue
			}
			text := normalize(strings.Join(lines, "\n"))
			k := key(rel, text)
			counts[k] = entry{count: counts[k].count + 1, path: rel, hash: strings.Split(k, "\t")[1]}
			if _, ok := first[k]; !ok {
				first[k] = finding{
					path: rel,
					line: fset.Position(group.Pos()).Line,
					text: text,
				}
			}
		}
		return nil
	})
	return counts, first, err
}

func readBaseline(path string) (map[string]entry, int, error) {
	f, err := os.Open(path) // #nosec G304 -- baseline path is a command-line flag
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]entry), 0, nil
		}
		return nil, 0, err
	}
	defer func() { _ = f.Close() }()
	out := make(map[string]entry)
	total := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != 3 {
			return nil, 0, fmt.Errorf("%s: malformed line %q", path, line)
		}
		count, err := strconv.Atoi(fields[0])
		if err != nil {
			return nil, 0, fmt.Errorf("%s: malformed count %q", path, fields[0])
		}
		k := fields[1] + "\t" + fields[2]
		out[k] = entry{count: count, path: fields[1], hash: fields[2]}
		total += count
	}
	if err := scanner.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, nil
}

func writeBaseline(path string, counts map[string]entry) error {
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		e := counts[k]
		fmt.Fprintf(&b, "%d\t%s\t%s\n", e.count, e.path, e.hash)
	}
	return os.WriteFile(path, []byte(b.String()), 0600)
}

func readLimit(path string) (int, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- limit path is a command-line flag
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("%s: %v", path, err)
	}
	return n, nil
}

func main() {
	baselinePath := flag.String("baseline", "tools/commentcheck/allowlist.txt", "baseline allowlist path")
	limitPath := flag.String("limit", "tools/commentcheck/limit.txt", "maximum permitted comment count")
	write := flag.Bool("write-baseline", false, "rewrite the baseline from the current tree")
	countOnly := flag.Bool("count", false, "print the current comment count and exit")
	flag.Parse()

	root := "."
	if flag.NArg() > 0 {
		root = flag.Arg(0)
	}

	counts, first, err := scan(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "commentcheck:", err)
		os.Exit(1)
	}

	if *countOnly {
		total := 0
		for _, e := range counts {
			total += e.count
		}
		fmt.Println(total)
		return
	}

	if *write {
		maxCount, limitErr := readLimit(*limitPath)
		if limitErr != nil {
			fmt.Fprintln(os.Stderr, "commentcheck:", limitErr)
			os.Exit(1)
		}
		total := 0
		for _, e := range counts {
			total += e.count
		}
		if total > maxCount {
			fmt.Fprintf(os.Stderr, "commentcheck: %d comments exceed the ratchet limit of %d; delete comments or lower the baseline, do not raise the limit\n", total, maxCount)
			os.Exit(1)
		}
		if writeErr := writeBaseline(*baselinePath, counts); writeErr != nil {
			fmt.Fprintln(os.Stderr, "commentcheck:", writeErr)
			os.Exit(1)
		}
		fmt.Printf("commentcheck: wrote %d comment(s) to %s\n", total, *baselinePath)
		return
	}

	baseline, baselineTotal, err := readBaseline(*baselinePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "commentcheck:", err)
		os.Exit(1)
	}
	limit, err := readLimit(*limitPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "commentcheck:", err)
		os.Exit(1)
	}
	if baselineTotal > limit {
		fmt.Fprintf(os.Stderr, "commentcheck: baseline holds %d comments, above the ratchet limit of %d\n", baselineTotal, limit)
		os.Exit(1)
	}

	failed := false
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		current := counts[k]
		allowed, ok := baseline[k]
		if !ok {
			f := first[k]
			fmt.Fprintf(os.Stderr, "%s:%d: comment is not in the allowlist: %s\n", f.path, f.line, f.text)
			failed = true
			continue
		}
		if current.count > allowed.count {
			f := first[k]
			fmt.Fprintf(os.Stderr, "%s:%d: comment appears %d times, allowlist permits %d: %s\n", f.path, f.line, current.count, allowed.count, f.text)
			failed = true
		}
	}
	for k, e := range baseline {
		if _, ok := counts[k]; !ok {
			fmt.Fprintf(os.Stderr, "%s: stale allowlist entry for a comment that no longer exists; run just comment-baseline to shrink it\n", e.path)
			failed = true
		}
	}
	if failed {
		os.Exit(1)
	}
	fmt.Printf("commentcheck: %d allowlisted comment(s), ratchet limit %d\n", baselineTotal, limit)
}

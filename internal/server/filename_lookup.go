package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"
	"unicode/utf8"
)

// Filename lookup observes names only: it does not establish content versions,
// file existence at later use, approval, or engineering/delivery acceptance.
const (
	filenameLookupEntries = 20000
	filenameLookupDepth   = 32
	filenameLookupBytes   = 12 << 10
)

type filenameLookupResult struct {
	Path               string   `json:"path"`
	Glob               string   `json:"glob"`
	Recursive          bool     `json:"recursive"`
	Matches            []string `json:"matches"`
	Complete           bool     `json:"complete"`
	Reasons            []string `json:"reasons,omitempty"`
	VisitedEntries     int      `json:"visitedEntries"`
	VisitedDirectories int      `json:"visitedDirectories"`
	SkippedSymlinks    int      `json:"skippedSymlinks"`
	SkippedPolicy      int      `json:"skippedPolicy"`
	Boundary           string   `json:"boundary"`
}

type filenameLookupBudget struct{ entries, depth, bytes int }

func lookupFilenames(ctx context.Context, root *os.Root, args map[string]any) (string, error) {
	return lookupFilenamesWithBudget(ctx, root, args, filenameLookupBudget{filenameLookupEntries, filenameLookupDepth, filenameLookupBytes})
}

func lookupFilenamesWithBudget(ctx context.Context, root *os.Root, args map[string]any, budget filenameLookupBudget) (string, error) {
	p := "."
	if v, ok := args["path"]; ok {
		s, valid := v.(string)
		if !valid {
			return "", errors.New("path must be a relative directory")
		}
		if strings.TrimSpace(s) != "" {
			p = strings.TrimSpace(s)
		}
	}
	if len(p) > 4096 {
		return "", errors.New("path exceeds 4096 bytes")
	}
	if err := safePath(p); err != nil {
		return "", err
	}
	p = path.Clean(p)
	glob, ok := args["glob"].(string)
	if !ok || glob == "" || len(glob) > 256 || strings.ContainsAny(glob, "/\\\x00") {
		return "", errors.New("glob must be a 1-256 byte filename pattern without directory separators")
	}
	pattern := strings.ToLower(glob)
	if _, err := path.Match(pattern, ""); err != nil {
		return "", fmt.Errorf("invalid filename glob: %w", err)
	}
	recursive := false
	if v, exists := args["recursive"]; exists {
		var valid bool
		recursive, valid = v.(bool)
		if !valid {
			return "", errors.New("recursive must be a boolean")
		}
	}
	limit := 100
	if v, exists := args["maxResults"]; exists {
		n, valid := v.(float64)
		if !valid || n < 1 || n > 200 || n != float64(int(n)) {
			return "", errors.New("maxResults must be an integer from 1 to 200")
		}
		limit = int(n)
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	result := filenameLookupResult{Path: p, Glob: glob, Recursive: recursive, Matches: []string{}, Complete: true,
		Boundary: "case-insensitive filename glob; regular files only; no symlink traversal; policy-excluded paths omitted; cooperative time budget; no file contents, write baseline, approval or artifact acceptance; incomplete results do not prove absence"}
	mark := func(reason string) {
		result.Complete = false
		for _, old := range result.Reasons {
			if old == reason {
				return
			}
		}
		if len(result.Reasons) < 8 {
			result.Reasons = append(result.Reasons, reason)
		}
	}
	base, closeBase, err := boundDirectoryRoot(root, p, false)
	if err != nil {
		return "", err
	}
	defer closeBase()
	outputBytes := 0
	var walk func(*os.Root, string, int) bool
	walk = func(dir *os.Root, relative string, depth int) bool {
		if err := ctx.Err(); err != nil {
			mark("cancelled_or_time_budget")
			return false
		}
		stream, err := dir.Open(".")
		if err != nil {
			mark("directory_read_error")
			return true
		}
		defer stream.Close()
		result.VisitedDirectories++
		for {
			entries, readErr := stream.ReadDir(128)
			for _, entry := range entries {
				if ctx.Err() != nil {
					mark("cancelled_or_time_budget")
					return false
				}
				if result.VisitedEntries >= budget.entries {
					mark("entry_budget")
					return false
				}
				result.VisitedEntries++
				name := entry.Name()
				full := path.Join(relative, name)
				if !utf8.ValidString(full) {
					mark("unrepresentable_filename")
					continue
				}
				if safePath(full) != nil || strings.HasPrefix(name, ".DS_Store") {
					result.SkippedPolicy++
					continue
				}
				info, statErr := dir.Lstat(name)
				if statErr != nil {
					mark("entry_stat_error")
					continue
				}
				if info.Mode()&os.ModeSymlink != 0 {
					result.SkippedSymlinks++
					continue
				}
				if info.IsDir() {
					if !recursive {
						continue
					}
					if depth >= budget.depth {
						mark("depth_budget")
						continue
					}
					child, closeChild, openErr := boundDirectoryRoot(dir, name, false)
					if openErr != nil {
						mark("directory_changed_or_unreadable")
						continue
					}
					more := walk(child, full, depth+1)
					closeChild()
					if !more {
						return false
					}
					continue
				}
				if !info.Mode().IsRegular() {
					continue
				}
				matched, _ := path.Match(pattern, strings.ToLower(name))
				if !matched {
					continue
				}
				// Discover the extra match before reporting a result-limit cut. A
				// complete directory with exactly limit matches remains complete.
				if len(result.Matches) >= limit {
					mark("result_budget")
					return false
				}
				encoded, _ := json.Marshal(full)
				if outputBytes+len(encoded)+1 > budget.bytes {
					mark("output_budget")
					return false
				}
				outputBytes += len(encoded) + 1
				result.Matches = append(result.Matches, full)
			}
			if readErr == io.EOF {
				return true
			}
			if readErr != nil {
				mark("directory_read_error")
				return true
			}
		}
	}
	walk(base, p, 0)
	encoded, err := json.Marshal(result)
	return string(encoded), err
}

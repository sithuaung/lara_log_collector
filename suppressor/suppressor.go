package suppressor

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/sithuaung/lara_log_collector/config"
)

type matchMode string

const (
	matchSubstring matchMode = "substring"
	matchRegex     matchMode = "regex"
)

type rule struct {
	raw      string
	subLower string
	re       *regexp.Regexp
}

// Suppressor tracks suppressed error counts without storing individual entries.
type Suppressor struct {
	mode            matchMode
	caseInsensitive bool
	rules           []rule
	loc             *time.Location

	mu        sync.Mutex
	date      string
	byApp     map[string]map[string]int64
}

// New creates a Suppressor from config. Returns nil if no patterns are provided.
func New(cfg config.SuppressConfig) (*Suppressor, error) {
	if len(cfg.Patterns) == 0 {
		return nil, nil
	}

	mode := matchMode(strings.ToLower(strings.TrimSpace(cfg.Match)))
	if mode == "" {
		mode = matchSubstring
	}
	if mode != matchSubstring && mode != matchRegex {
		return nil, fmt.Errorf("invalid suppress.match: %q (expected \"substring\" or \"regex\")", cfg.Match)
	}

	locName := strings.TrimSpace(cfg.Timezone)
	if locName == "" {
		locName = "Asia/Bangkok"
	}
	loc, err := time.LoadLocation(locName)
	if err != nil {
		return nil, fmt.Errorf("load timezone %q: %w", locName, err)
	}

	rules := make([]rule, 0, len(cfg.Patterns))
	for _, p := range cfg.Patterns {
		pat := strings.TrimSpace(p)
		if pat == "" {
			continue
		}
		r := rule{raw: pat}
		if mode == matchRegex {
			exp := pat
			if cfg.CaseInsensitive {
				exp = "(?i)" + exp
			}
			re, err := regexp.Compile(exp)
			if err != nil {
				return nil, fmt.Errorf("compile suppress pattern %q: %w", pat, err)
			}
			r.re = re
		} else {
			if cfg.CaseInsensitive {
				r.subLower = strings.ToLower(pat)
			} else {
				r.subLower = pat
			}
		}
		rules = append(rules, r)
	}
	if len(rules) == 0 {
		return nil, nil
	}

	return &Suppressor{
		mode:            mode,
		caseInsensitive: cfg.CaseInsensitive,
		rules:           rules,
		loc:             loc,
		byApp:           make(map[string]map[string]int64),
	}, nil
}

// Match checks if message should be suppressed. Returns true and the rule key.
func (s *Suppressor) Match(message string) (bool, string) {
	if s == nil || message == "" {
		return false, ""
	}
	msg := message
	if s.caseInsensitive {
		msg = strings.ToLower(message)
	}

	for _, r := range s.rules {
		if s.mode == matchRegex {
			if r.re != nil && r.re.MatchString(message) {
				return true, r.raw
			}
			continue
		}
		if r.subLower != "" && strings.Contains(msg, r.subLower) {
			return true, r.raw
		}
	}
	return false, ""
}

// Inc increments the counter for app+rule if date matches. Resets on date change.
func (s *Suppressor) Inc(appName, ruleKey string) {
	if s == nil || appName == "" || ruleKey == "" {
		return
	}
	now := time.Now().In(s.loc)
	date := now.Format("2006-01-02")

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.date != date {
		s.date = date
		s.byApp = make(map[string]map[string]int64)
	}

	byRule := s.byApp[appName]
	if byRule == nil {
		byRule = make(map[string]int64)
		s.byApp[appName] = byRule
	}
	byRule[ruleKey]++
}

// Snapshot returns a copy of counts for the current date.
func (s *Suppressor) Snapshot() (string, map[string]map[string]int64) {
	if s == nil {
		return "", nil
	}
	now := time.Now().In(s.loc)
	date := now.Format("2006-01-02")

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.date != date {
		s.date = date
		s.byApp = make(map[string]map[string]int64)
	}

	out := make(map[string]map[string]int64, len(s.byApp))
	for app, byRule := range s.byApp {
		copied := make(map[string]int64, len(byRule))
		for k, v := range byRule {
			copied[k] = v
		}
		out[app] = copied
	}
	return date, out
}

// Location returns the suppressor timezone.
func (s *Suppressor) Location() *time.Location {
	if s == nil {
		return time.UTC
	}
	return s.loc
}

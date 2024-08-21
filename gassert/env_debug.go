//go:build debug

package gassert

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
)

// Env is an alias to the Environment type.
// This allows consumers to have a field of type Env
// which is an empty struct in non-debug builds
// and is a pointer to a proper assertion environment in debug builds.
type Env = *Environment

// Environment contains the set of rules for evaluating
// whether to perform certain assertions at runtime.
//
// Methods on Environment are safe for concurrent use,
// except UseCaching, which must be called before any other methods,
// if called at all.
type Environment struct {
	// Prefix matches strings ending in the wildcard.
	// (The wildcard is not stored as part of the sub-slice.)
	prefixes [][]string

	// Exclusions from prefix matches.
	excludes [][]string

	// Exact matches.
	exacts [][]string

	// If caching is enabled, the cache is protected by this RWMutex.
	// Nil cache indicates that caching is disabled.
	mu    sync.RWMutex
	cache map[string]bool

	// By default, errors cause a panic.
	// OnlyLogFailures sets the log field,
	// indicating that HandleAssertionFailure should only log instead of panicking.
	log *slog.Logger
}

// EnvironmentFromString parses a comma-separated string containing enable rules.
func EnvironmentFromString(in string) (*Environment, error) {
	var e Environment
	if in == "" {
		// Splitting the empty string will produce a slice containing one empty string,
		// which will cause an empty rule error.
		// So return early here as a special case.
		return &e, nil
	}

	for _, r := range strings.Split(in, ",") {
		if err := e.parseSingleRule(r); err != nil {
			return nil, err
		}
	}
	e.sort()

	return &e, nil
}

// ParseEnvironment parses environment rules from r, one line at a time.
// Compared to [EnvironmentFromString], ParseEnvironment allows for comments and blank lines.
func ParseEnvironment(r io.Reader) (*Environment, error) {
	var e Environment

	scanner := bufio.NewScanner(r)
	// Scanner buffer defaults to 64k.
	// We should never have a rule anywhere close to that,
	// so reduce allocation a little here.
	scanner.Buffer(make([]byte, 0, 512), 511)
	lineIdx := 0
	nErrs := 0
	const errLimit = 5
	var errs error
	for scanner.Scan() {
		lineIdx++
		// The line will contain a trailing newline,
		// unless it's the last line and the file doesn't end with a newline.
		line := strings.TrimSuffix(scanner.Text(), "\n")

		// Blank or comment lines are allowed in parser.
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if err := e.parseSingleRule(line); err != nil {
			errs = errors.Join(errs, err)
			nErrs++
			if nErrs >= errLimit {
				errs = errors.Join(errs, fmt.Errorf("stopped parsing after %d errors", nErrs))
				return nil, errs
			}
		}
	}

	if errs != nil {
		return nil, errs
	}

	return &e, nil
}

func (e *Environment) parseSingleRule(r string) error {
	if len(r) == 0 {
		return errors.New("received empty rule")
	}

	if strings.Contains(r, "..") {
		return fmt.Errorf("invalid rule %q: dot-separated sections may not be empty", r)
	}

	if strings.Contains(r, "!") {
		exRule, wasPrefix := strings.CutPrefix(r, "!")
		if !wasPrefix {
			return fmt.Errorf("invalid rule %q: ! may only occur at the start of the rule, indicating an exclusion", r)
		}
		if strings.Contains(exRule, "*") {
			// We could potentially relax this later, but that seems too complex.
			return fmt.Errorf("invalid rule %q: wildcards are not allowed with exclusion rules", r)
		}
		e.excludes = append(e.excludes, strings.Split(exRule, "."))
		return nil
	}

	nStars := strings.Count(r, "*")
	if nStars > 1 {
		return fmt.Errorf("invalid rule %q: may contain at most one *, and it must be at the end", r)
	}
	if nStars == 1 {
		if r == "*" {
			// Special case, just add this directly.
			// Using a zero-length slice here because it seems inappropriate to use nil.
			e.prefixes = append(e.prefixes, []string{})
			return nil
		}

		// Looks like a prefix, but is it?
		p, isPrefix := strings.CutSuffix(r, ".*")
		if !isPrefix {
			return fmt.Errorf("invalid rule %q: * only allowed as last element of dot-separated rule", r)
		}
		// Okay, it looked like a valid prefix.
		e.prefixes = append(e.prefixes, strings.Split(p, "."))
		return nil
	}

	// No wildcards, so at this point we are expecting an exact match.
	e.exacts = append(e.exacts, strings.Split(r, "."))
	return nil
}

// UseCaching configures e to always return the last calculated result for Enabled.
//
// UseCaching must be called before any concurrent use of e.
// Once caching is enabled, it may not be disabled.
func (e *Environment) UseCaching() {
	if e.cache != nil {
		panic(errors.New("BUG: UseCaching called twice"))
	}

	e.cache = make(map[string]bool)
}

// OnlyLogFailures configures e to log assertion failures
// at Error level to the given logger,
// instead of the default behavior of panicking.
//
// OnlyLogFailures must be called before any concurrent use of e.
// Once failure logging is enabled, it may not be disabled.
func (e *Environment) OnlyLogFailures(log *slog.Logger) {
	e.log = log
}

// HandleAssertionFailure marks the given error as a failure.
// The default behavior is to panic.
// However, if e.OnlyLogFailures was called before,
// then the error is only logged.
//
// If the given error is nil, HandleAssertionFailure panics.
func (e *Environment) HandleAssertionFailure(err error) {
	if err == nil {
		panic(errors.New("BUG: HandleAssertionFailure called with nil error"))
	}

	if e.log == nil {
		panic(fmt.Errorf("assertion failure: %w", err))
	}

	e.log.Error("Assertion failure", "err", err)
}

// Enabled reports whether the given rule is enabled.
// First, the prefixes are considered.
// If the rule would be enabled under a prefix rule,
// then e checks for any exclusion rule that would invalidate
// its enabled state.
// If an exclusion rule invalidates the prefix match,
// Enabled reports false.
//
// If no prefix rule matched,
// then Enabled reports whether any exact rule matches.
func (e *Environment) Enabled(rule string) bool {
	// If this environment was configured without any rules,
	// just check two lengths and return immediately.
	if len(e.prefixes) == 0 && len(e.exacts) == 0 {
		return false
	}

	if e.cache == nil {
		// If not caching, just evaluate enabled every time.
		return e.enabled(rule)
	}

	// Caching is enabled.
	// First, see if the value already exists.
	val, ok := e.tryCache(rule)
	if ok {
		return val
	}

	// It's not in the cache, so we have to take a write lock.
	e.mu.Lock()
	defer e.mu.Unlock()

	// Now that we have the lock, we can check once more in case
	// there was a concurrent write to the same key.
	if val, ok := e.cache[rule]; ok {
		return val
	}

	// Didn't have it.
	// Put it in the cache and return the value.
	val = e.enabled(rule)
	e.cache[rule] = val
	return val
}

// enabled evaluates rule without using the mutex or the cache.
func (e *Environment) enabled(rule string) bool {
	ruleParts := strings.Split(rule, ".")

	isPrefixMatch := false
	for _, p := range e.prefixes {
		if len(p) > len(ruleParts)-1 {
			// Prefixes are assumed to be sorted.
			// We've advanced too far in the prefix list.
			break
		}

		// Prefix length is correct.
		// Is it actually a prefix?
		if slices.Equal(p, ruleParts[:len(p)]) {
			isPrefixMatch = true
			break
		}
	}

	if isPrefixMatch {
		// The prefix matched, but there may be an exclusion rule to apply.
		// Exclusions are treated as negative exact matches, for now.
		for _, exclude := range e.excludes {
			if len(exclude) < len(ruleParts) {
				// Haven't found our length yet.
				continue
			}

			if len(exclude) > len(ruleParts) {
				// We've advanced too far in excludes.
				// So, the wildcard match takes effect.
				return true
			}

			// Length matches, so do the values match too?
			if slices.Equal(exclude, ruleParts) {
				// Exclude rule takes precedence.
				return false
			}
		}

		// No exclude rule applied.
		return true
	}

	// No prefix matched, so look for exact matches.
	for _, exact := range e.exacts {
		// Exacts are also assumed to be sorted.
		if len(exact) < len(ruleParts) {
			// Haven't found our length yet.
			continue
		}

		if len(exact) > len(ruleParts) {
			// Too far.
			return false
		}

		// Length matches, so do the values match too?
		if slices.Equal(exact, ruleParts) {
			return true
		}
	}

	// Nothing matched.
	return false
}

// tryCache retrieves the rule value from the cache under a read lock,
// reporting the value and whether it was present.
func (e *Environment) tryCache(rule string) (val, ok bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	val, ok = e.cache[rule]
	return val, ok
}

// sort sorts the slice sets in e, smallest first.
func (e *Environment) sort() {
	slices.SortFunc(e.prefixes, stringSliceLenSort)
	slices.SortFunc(e.excludes, stringSliceLenSort)
	slices.SortFunc(e.exacts, stringSliceLenSort)
}

func stringSliceLenSort(a, b []string) int {
	if len(a) < len(b) {
		return -1
	}
	if len(b) > len(a) {
		return 1
	}
	return 0
}

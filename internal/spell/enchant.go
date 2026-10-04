package spell

/*
#cgo LDFLAGS: -ldl
#include <dlfcn.h>
#include <stdlib.h>
#include <sys/types.h>

typedef void *(*broker_init_t)(void);
typedef void *(*request_dict_t)(void *, const char *);
typedef int (*dict_exists_t)(void *, const char *);
typedef int (*check_t)(void *, const char *, ssize_t);
typedef char **(*suggest_t)(void *, const char *, ssize_t, size_t *);
typedef void (*free_list_t)(void *, char **);
typedef void (*add_t)(void *, const char *, ssize_t);

static broker_init_t p_init;
static request_dict_t p_request;
static dict_exists_t p_exists;
static check_t p_check;
static suggest_t p_suggest;
static free_list_t p_free;
static add_t p_add, p_session;

// klient_enchant_load opens libenchant-2 and looks up the functions Klient
// uses; 0 when the library is missing.
static int klient_enchant_load(void) {
	void *h = dlopen("libenchant-2.so.2", RTLD_NOW | RTLD_LOCAL);
	if (!h) return 0;
	p_init = (broker_init_t)dlsym(h, "enchant_broker_init");
	p_request = (request_dict_t)dlsym(h, "enchant_broker_request_dict");
	p_exists = (dict_exists_t)dlsym(h, "enchant_broker_dict_exists");
	p_check = (check_t)dlsym(h, "enchant_dict_check");
	p_suggest = (suggest_t)dlsym(h, "enchant_dict_suggest");
	p_free = (free_list_t)dlsym(h, "enchant_dict_free_string_list");
	p_add = (add_t)dlsym(h, "enchant_dict_add");
	p_session = (add_t)dlsym(h, "enchant_dict_add_to_session");
	return p_init && p_request && p_exists && p_check && p_suggest && p_free && p_add && p_session;
}

static void *k_init(void) { return p_init(); }
static void *k_request(void *b, const char *tag) { return p_request(b, tag); }
static int k_exists(void *b, const char *tag) { return p_exists(b, tag); }
static int k_check(void *d, const char *w) { return p_check(d, w, -1); }
static char **k_suggest(void *d, const char *w, size_t *n) { return p_suggest(d, w, -1, n); }
static void k_free(void *d, char **l) { p_free(d, l); }
static void k_add(void *d, const char *w) { p_add(d, w, -1); }
static void k_session(void *d, const char *w) { p_session(d, w, -1); }
static char *k_at(char **l, size_t i) { return l[i]; }
*/
import "C"

import (
	"sort"
	"strings"
	"sync"
	"unsafe"
)

var (
	loadOnce sync.Once
	broker   unsafe.Pointer
	mu       sync.Mutex // Enchant is not thread-safe
)

func load() bool {
	loadOnce.Do(func() {
		if C.klient_enchant_load() != 0 {
			broker = C.k_init()
		}
	})
	return broker != nil
}

// Languages Klient offers, in order of preference for suggestions.
var Languages = []string{"cs_CZ", "en_US", "en_GB", "sk_SK", "de_DE"}

// Available returns the Languages with an installed dictionary.
func Available() []string {
	if !load() {
		return nil
	}
	mu.Lock()
	defer mu.Unlock()
	var out []string
	for _, l := range Languages {
		cs := C.CString(l)
		ok := C.k_exists(broker, cs) != 0
		C.free(unsafe.Pointer(cs))
		if ok {
			out = append(out, l)
		}
	}
	return out
}

// Checker checks words in several languages at once: a word is right when
// any of the dictionaries knows it (people write Czech and English mixed).
type Checker struct {
	langs []string
	dicts []unsafe.Pointer
}

// New opens the dictionaries of langs that are installed; nil when none is.
func New(langs []string) *Checker {
	if !load() {
		return nil
	}
	mu.Lock()
	defer mu.Unlock()
	c := &Checker{}
	for _, l := range langs {
		cs := C.CString(l)
		d := C.k_request(broker, cs)
		C.free(unsafe.Pointer(cs))
		if d != nil {
			c.langs = append(c.langs, l)
			c.dicts = append(c.dicts, d)
		}
	}
	if len(c.dicts) == 0 {
		return nil
	}
	return c
}

// Languages are the dictionaries in use.
func (c *Checker) Languages() []string { return c.langs }

// Check reports whether a word is spelled right.
func (c *Checker) Check(word string) bool {
	cs := C.CString(word)
	defer C.free(unsafe.Pointer(cs))
	mu.Lock()
	defer mu.Unlock()
	for _, d := range c.dicts {
		if C.k_check(d, cs) == 0 {
			return true
		}
	}
	return false
}

// Suggest returns up to max corrections, from the first dictionary first.
func (c *Checker) Suggest(word string, max int) []string {
	cs := C.CString(word)
	defer C.free(unsafe.Pointer(cs))
	mu.Lock()
	defer mu.Unlock()
	var out []string
	seen := map[string]bool{}
	for _, d := range c.dicts {
		var n C.size_t
		list := C.k_suggest(d, cs, &n)
		if list == nil {
			continue
		}
		for i := C.size_t(0); i < n; i++ {
			s := C.GoString(C.k_at(list, i))
			if s != "" && !seen[strings.ToLower(s)] {
				seen[strings.ToLower(s)] = true
				out = append(out, s)
			}
		}
		C.k_free(d, list)
	}
	// The closest corrections first, whichever dictionary they come from
	// ("tommorow": English "tomorrow" before Czech "promotor").
	sort.SliceStable(out, func(i, j int) bool { return distance(word, out[i]) < distance(word, out[j]) })
	if len(out) > max {
		out = out[:max]
	}
	return out
}

// Add puts a word into the personal dictionary (kept by Enchant for all
// applications), under the first language.
func (c *Checker) Add(word string) {
	cs := C.CString(word)
	defer C.free(unsafe.Pointer(cs))
	mu.Lock()
	defer mu.Unlock()
	C.k_add(c.dicts[0], cs)
}

// Ignore accepts a word until Klient quits.
func (c *Checker) Ignore(word string) {
	cs := C.CString(word)
	defer C.free(unsafe.Pointer(cs))
	mu.Lock()
	defer mu.Unlock()
	for _, d := range c.dicts {
		C.k_session(d, cs)
	}
}

// distance is the optimal string alignment distance (edits and swaps of
// neighbouring letters), ignoring case.
func distance(a, b string) int {
	x, y := []rune(strings.ToLower(a)), []rune(strings.ToLower(b))
	d := make([][]int, len(x)+1)
	for i := range d {
		d[i] = make([]int, len(y)+1)
		d[i][0] = i
	}
	for j := range d[0] {
		d[0][j] = j
	}
	for i := 1; i <= len(x); i++ {
		for j := 1; j <= len(y); j++ {
			cost := 1
			if x[i-1] == y[j-1] {
				cost = 0
			}
			d[i][j] = min(d[i-1][j]+1, d[i][j-1]+1, d[i-1][j-1]+cost)
			if i > 1 && j > 1 && x[i-1] == y[j-2] && x[i-2] == y[j-1] {
				d[i][j] = min(d[i][j], d[i-2][j-2]+1)
			}
		}
	}
	return d[len(x)][len(y)]
}

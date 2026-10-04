package spell

import (
	"reflect"
	"testing"
)

func TestWords(t *testing.T) {
	text := "Dobrý den, posílám fakturu č. 2026/15 na www.example.org a jan@example.cz.\n" +
		"> citovaný řádek s chibou\n" +
		"Formát A4, PDF, česko-slovenský don't https://x.cz/a Ahoj!"
	var got []string
	for _, w := range Words(text) {
		got = append(got, w.Text)
	}
	want := []string{"Dobrý", "den", "posílám", "fakturu", "na", "Formát", "česko-slovenský", "don't", "Ahoj"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Words = %q\nwant      %q", got, want)
	}
	// Offsets are in characters, not bytes.
	w := Words("Žluťoučký kůň")[1]
	if w.Start != 10 || w.End != 13 || w.Text != "kůň" {
		t.Errorf("offsets %+v", w)
	}
}

// With the system dictionaries (skipped where they are not installed).
func TestChecker(t *testing.T) {
	c := New([]string{"cs_CZ", "en_US"})
	if c == nil || len(c.Languages()) < 2 {
		t.Skip("Enchant with Czech and English dictionaries is not installed")
	}
	for _, w := range []string{"pravopis", "kůň", "spelling", "Praha"} {
		if !c.Check(w) {
			t.Errorf("%q marked wrong", w)
		}
	}
	for _, w := range []string{"pravopys", "speling"} {
		if c.Check(w) {
			t.Errorf("%q accepted", w)
		}
	}
	if s := c.Suggest("pravopys", 5); len(s) == 0 || s[0] != "pravopis" {
		t.Errorf("suggestions for pravopys: %q", s)
	}
	if s := c.Suggest("tommorow", 5); len(s) == 0 || s[0] != "tomorrow" {
		t.Errorf("suggestions for tommorow: %q", s)
	}
	c.Ignore("Klientovi")
	if !c.Check("Klientovi") {
		t.Error("an ignored word is still wrong")
	}
}

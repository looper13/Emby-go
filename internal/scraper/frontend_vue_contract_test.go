package scraper

import (
	"encoding/json"
	"os"
	"testing"
)

// Vue consumes the same input corpus; these outputs come from the actual Go implementation.
func TestVueMigrationNumberContract(t *testing.T) {
	data, err := os.ReadFile("../../vue-migration/fixtures/ct/ct-01-number.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			ID    string `json:"id"`
			Input struct {
				Title  string `json:"title"`
				Number string `json:"number"`
			} `json:"input"`
			Expected string `json:"expected_join"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	output := make(map[string]string)
	for _, c := range fixture.Cases {
		got := joinNumberTitle(c.Input.Number, c.Input.Title)
		output[c.ID] = got
		if got != c.Expected {
			t.Errorf("%s: actual Go join=%q expected=%q", c.ID, got, c.Expected)
		}
	}
	encoded, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("CT-01 Go output:", string(encoded))
}

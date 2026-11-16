package scraper

import (
	"encoding/json"
	"os"
	"testing"
)

// Vue 与 Go 使用同一套输入夹具，断言结果来自实际 Go 实现。
func TestFrontendNumberContract(t *testing.T) {
	data, err := os.ReadFile("../../frontend/tests/fixtures/ct/ct-01-number.json")
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

package server

import (
	"encoding/json"
	"os"
	"testing"
)

func TestVueMigrationEntityContract(t *testing.T) {
	data, err := os.ReadFile("../../vue-migration/fixtures/ct/ct-02-entity-id.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Cases []struct {
			ID    string `json:"id"`
			Input struct {
				Kind string `json:"kind"`
				Name string `json:"name"`
			} `json:"input"`
			Expected string `json:"expected"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	output := make(map[string]string)
	for _, c := range fixture.Cases {
		got := entityId(c.Input.Kind, c.Input.Name)
		output[c.ID] = got
		if got != c.Expected {
			t.Errorf("%s: actual Go entity=%q expected=%q", c.ID, got, c.Expected)
		}
	}
	encoded, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("CT-02 Go output:", string(encoded))
}

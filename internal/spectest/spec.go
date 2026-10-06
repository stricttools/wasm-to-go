package spectest

import (
	"encoding/json"
	"os"
)

type specTest struct {
	Commands []specCommand `json:"commands"`
}

type specCommand struct {
	Type     string `json:"type"`
	Line     int    `json:"line"`
	Filename string `json:"filename"`
	Action   struct {
		Field string    `json:"field"`
		Args  []specArg `json:"args"`
	} `json:"action"`
	Text     string    `json:"text"`
	Expected []specArg `json:"expected"`
}

type specArg struct {
	Type     string   `json:"type"`
	LaneType string   `json:"lane_type"`
	Value    string   `json:"-"`
	Lanes    []string `json:"-"` // a v128's lanes, in lane_type
}

func (a *specArg) UnmarshalJSON(b []byte) error {
	var raw struct {
		Type     string          `json:"type"`
		LaneType string          `json:"lane_type"`
		Value    json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	a.Type, a.LaneType = raw.Type, raw.LaneType
	if raw.Value == nil {
		return nil
	}
	if a.Type == "v128" {
		return json.Unmarshal(raw.Value, &a.Lanes)
	}
	return json.Unmarshal(raw.Value, &a.Value)
}

func parseSpec(file string) (*specTest, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	spec := new(specTest)
	if err := json.NewDecoder(f).Decode(spec); err != nil {
		return nil, err
	}
	return spec, nil
}

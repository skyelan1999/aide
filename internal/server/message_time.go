package server

import "encoding/json"

// Historical message extensions were ignored. A non-string timestamp must not
// make an otherwise readable legacy session fail deserialization after upgrade.
func (m *Message) UnmarshalJSON(raw []byte) error {
	type wire Message
	value := struct {
		*wire
		CreatedAt json.RawMessage `json:"createdAt"`
	}{wire: (*wire)(m)}
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	if len(value.CreatedAt) > 0 {
		var at string
		if json.Unmarshal(value.CreatedAt, &at) == nil {
			m.CreatedAt = at
		} else {
			m.CreatedAt = ""
		}
	}
	return nil
}

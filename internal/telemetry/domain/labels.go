package domain

import "encoding/json"

// CanonicalLabels serializes labels as a JSON object with sorted keys. encoding/json sorts map
// keys, so equal label sets always yield identical text: it is part of a record's identity.
func CanonicalLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return "{}"
	}
	out, err := json.Marshal(labels)
	if err != nil { // a map[string]string cannot fail to marshal
		return "{}"
	}
	return string(out)
}

// ParseLabels is the inverse of CanonicalLabels; malformed input yields an empty set.
func ParseLabels(text string) map[string]string {
	labels := map[string]string{}
	if err := json.Unmarshal([]byte(text), &labels); err != nil || labels == nil {
		return map[string]string{}
	}
	return labels
}

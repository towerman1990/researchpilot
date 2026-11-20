package model

// ResearchPreferences contains the account defaults copied into a new task.
type ResearchPreferences struct {
	Sources   string `json:"sources"`
	Detail    string `json:"detail"`
	Structure string `json:"structure"`
}

package execution

import "researchpilot/internal/repository/model"

const preferencePriority = " Explicit requirements in the current research goal take precedence over saved preferences."

func sourcePreference(p model.ResearchPreferences) string {
	switch p.Sources {
	case "official":
		return " Saved source preference: prioritize official documentation and upstream release notes." + preferencePriority
	case "github":
		return " Saved source preference: prioritize public upstream GitHub repositories, release records and implementation evidence." + preferencePriority
	default:
		return ""
	}
}

func reportPreferences(p model.ResearchPreferences) string {
	var prompt string
	switch p.Detail {
	case "brief":
		prompt += " Saved report detail preference: keep findings concise and focus on essential conclusions."
	case "detailed":
		prompt += " Saved report detail preference: include supported implementation details, conditions and trade-offs."
	}
	switch p.Structure {
	case "conclusion_first":
		prompt += " Saved report structure preference: lead the findings with the main conclusion, then present supporting findings."
	case "bullets":
		prompt += " Saved report structure preference: return an additional JSON field layout with value bullets for a list of findings. Use paragraphs instead if requested by the current research goal."
	}
	if prompt == "" {
		return ""
	}
	return prompt + preferencePriority
}

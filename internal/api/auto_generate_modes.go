package api

import "strings"

const (
	AutoGenerateModeNarration   = "narration"
	AutoGenerateModeHighQuality = "high_quality"
	AutoGenerateModeStoryboard  = "storyboard"
	AutoGenerateModeR2V         = "r2v"
)

func normalizeAutoGenerateGenerationMode(raw string, allowCharacterSpeech bool) string {
	switch strings.TrimSpace(raw) {
	case AutoGenerateModeNarration:
		return AutoGenerateModeNarration
	case AutoGenerateModeHighQuality:
		return AutoGenerateModeHighQuality
	case AutoGenerateModeStoryboard:
		return AutoGenerateModeStoryboard
	case AutoGenerateModeR2V:
		return AutoGenerateModeR2V
	}
	if allowCharacterSpeech {
		return AutoGenerateModeHighQuality
	}
	return AutoGenerateModeNarration
}

func autoGenerateModeAllowsCharacterSpeech(mode string) bool {
	switch normalizeAutoGenerateGenerationMode(mode, false) {
	case AutoGenerateModeHighQuality, AutoGenerateModeStoryboard, AutoGenerateModeR2V:
		return true
	default:
		return false
	}
}

func autoGenerateModeRequiresEmptyNarration(mode string) bool {
	_ = mode
	return false
}

func autoGenerateModeUsesFlowingVideoPrompt(mode string) bool {
	switch normalizeAutoGenerateGenerationMode(mode, false) {
	case AutoGenerateModeHighQuality, AutoGenerateModeStoryboard, AutoGenerateModeR2V:
		return true
	default:
		return false
	}
}

func inferGenerationModeFromPayload(payload *lightweightStoryResponse) string {
	if payload == nil {
		return AutoGenerateModeNarration
	}
	hasFlowingVideoPrompt := false
	for _, scene := range payload.Scenes {
		if strings.TrimSpace(scene.VideoPrompt) != "" {
			if _, err := parseFlowingVideoPrompt(scene.VideoPrompt); err == nil {
				hasFlowingVideoPrompt = true
			}
		}
	}
	if hasFlowingVideoPrompt {
		return AutoGenerateModeHighQuality
	}
	return AutoGenerateModeNarration
}

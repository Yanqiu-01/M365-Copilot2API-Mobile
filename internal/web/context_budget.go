package web

import (
	"fmt"
	"strings"

	"m365-copilot2api/internal/chathub"
)

// configuredContextBudget is the usable input context recovered from the APK:
// configured context window less the configured output reservation.
func configuredContextBudget() int {
	settings := currentSettings()
	window := settings.ContextWindow
	output := settings.MaxOutputTokens
	if window < 1024 {
		window = 128000
	}
	if output < 1 || output >= window {
		output = 16384
		if output >= window {
			output = window / 4
		}
	}
	return window - output
}

func messageTokenCost(message oaiMsg, model string) int {
	count, _ := tokenEstimator(model)
	cost := messageProtocolTokens + count(strings.TrimSpace(message.Role))
	cost += serializedTokenCount(message.Content, count)
	cost += count(message.Name)
	cost += count(message.ToolCallID)
	for _, call := range message.ToolCalls {
		cost += serializedTokenCount(call, count)
	}
	return cost
}

func toolSchemaTokenCost(tools []chathub.Tool, toolChoice any, model string) int {
	count, _ := tokenEstimator(model)
	cost := 0
	for _, tool := range tools {
		cost += toolProtocolTokens + serializedTokenCount(tool, count)
	}
	if toolChoice != nil {
		cost += toolChoiceProtocolTokens + serializedTokenCount(toolChoice, count)
	}
	return cost
}

func isInstructionRole(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "system", "developer":
		return true
	default:
		return false
	}
}

type messageGroup struct {
	messages []oaiMsg
	cost     int
}

// messageGroups preserves message turn boundaries: system/developer messages
// are returned separately, while every user message and its following
// assistant/tool messages form one evictable group.
func messageGroups(messages []oaiMsg, model string) (instructions []oaiMsg, groups []messageGroup) {
	var current *messageGroup
	for _, message := range messages {
		if isInstructionRole(message.Role) {
			instructions = append(instructions, message)
			continue
		}
		if strings.EqualFold(strings.TrimSpace(message.Role), "user") || current == nil {
			groups = append(groups, messageGroup{})
			current = &groups[len(groups)-1]
		}
		current.messages = append(current.messages, message)
		current.cost += messageTokenCost(message, model)
	}
	return instructions, groups
}

func trimMessagesToContext(messages []oaiMsg, tools []chathub.Tool, toolChoice any, model string) ([]oaiMsg, error) {
	return trimMessagesWithBudget(messages, tools, toolChoice, model, configuredContextBudget())
}

// trimMessagesWithBudget keeps all instruction messages and the newest complete
// conversation turns that fit the budget. It never returns a dangling tool
// result without its preceding user/assistant context.
func trimMessagesWithBudget(messages []oaiMsg, tools []chathub.Tool, toolChoice any, model string, budget int) ([]oaiMsg, error) {
	if budget < 1 {
		return nil, fmt.Errorf("context budget must be positive")
	}
	instructions, groups := messageGroups(messages, model)
	used := toolSchemaTokenCost(tools, toolChoice, model)
	for _, message := range instructions {
		used += messageTokenCost(message, model)
	}
	if used > budget {
		return nil, fmt.Errorf("instruction and tool schemas exceed context budget (%d > %d tokens)", used, budget)
	}

	keepFrom := len(groups)
	for i := len(groups) - 1; i >= 0; i-- {
		if used+groups[i].cost > budget {
			break
		}
		used += groups[i].cost
		keepFrom = i
	}

	out := make([]oaiMsg, 0, len(instructions)+len(messages))
	out = append(out, instructions...)
	for _, group := range groups[keepFrom:] {
		out = append(out, group.messages...)
	}
	if len(out) == 0 && len(messages) > 0 {
		return nil, fmt.Errorf("latest message exceeds context budget (%d tokens)", budget)
	}
	return out, nil
}

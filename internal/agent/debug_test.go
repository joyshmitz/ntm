package agent

import (
	"fmt"
	"testing"
)

func TestDebugIdle(t *testing.T) {
	p := NewParser()
	output := "Task completed successfully.\nWhat would you like me to do next?\nHuman: "
	state, _ := p.Parse(output)
	fmt.Printf("Type: %v\n", state.Type)
	fmt.Printf("IsIdle: %v\n", state.IsIdle)
	fmt.Printf("IsWorking: %v\n", state.IsWorking)
	fmt.Printf("WorkIndicators: %v\n", state.WorkIndicators)
}

package resource

import "testing"

func TestCreateTaskResourceIsCommandAction(t *testing.T) {
	h := NewTaskDefHistory()
	var r Resource = NewCreateTaskResource(&fakeTaskcluster{}, h)
	ca, ok := r.(CommandAction)
	if !ok {
		t.Fatal("CreateTaskResource must implement CommandAction")
	}
	if a := ca.CommandAction(); a.Perform == nil || a.Prompt == "" {
		t.Fatalf("incomplete action: %+v", a)
	}
}

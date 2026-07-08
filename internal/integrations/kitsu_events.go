package integrations

import (
	"context"
	"encoding/json"
	"strings"
)

// ExternalAssignment is the minimal shape an event listener emits when an
// assignment changes in the external system. ProjectID + TaskID are enough
// to look up the corresponding Clustta asset; PersonIDs is the authoritative
// post-event set of assignees. Status carries the external task status id.
type ExternalAssignment struct {
	IntegrationId string
	ProjectId     string
	TaskId        string
	TaskTypeId    string
	TaskTypeName  string
	PersonIds     []string
	Status        string
}

// KitsuEvent is the decoded payload from a Kitsu socket.io event.
// The native event only carries IDs; full task state is fetched via REST.
type KitsuEvent struct {
	Name      string // e.g. "task:assignation", "task:unassignation", "task:update"
	TaskId    string
	ProjectId string
	PersonId  string
}

// KitsuEventSubscriber abstracts the underlying transport (socket.io or
// long-poll). The production implementation is NewKitsuSocketSubscriber;
// the interface keeps the listener decoupled so future transports or test
// doubles can be plugged in without touching it.
type KitsuEventSubscriber interface {
	// Subscribe connects to the Kitsu event stream and emits events on out
	// until ctx is cancelled. It returns when the stream ends or errors.
	Subscribe(ctx context.Context, out chan<- KitsuEvent) error
}

// GetTask fetches a single task by ID from Kitsu and returns the assignment-
// relevant fields. Required because the event stream carries only IDs; the
// authoritative assignment state lives on the task resource.
func (k *KitsuClient) GetTask(token, apiUrl, taskId string) (ExternalAssignment, error) {
	apiUrl = strings.TrimSuffix(apiUrl, "/")
	data, err := k.get(token, apiUrl+"/api/data/tasks/"+taskId)
	if err != nil {
		return ExternalAssignment{}, err
	}
	var task kitsuTask
	if err := json.Unmarshal(data, &task); err != nil {
		return ExternalAssignment{}, err
	}
	return ExternalAssignment{
		IntegrationId: "kitsu",
		ProjectId:     task.ProjectID,
		TaskId:        task.ID,
		TaskTypeId:    task.TaskTypeID,
		TaskTypeName:  task.TaskTypeName,
		PersonIds:     task.Assignees,
		Status:        task.TaskStatusID,
	}, nil
}

// GetPerson fetches a single person (user) by ID from Kitsu and returns
// the email. Used by the assignee resolver to map external person IDs to
// Clustta user IDs.
func (k *KitsuClient) GetPerson(token, apiUrl, personId string) (ExternalUser, error) {
	apiUrl = strings.TrimSuffix(apiUrl, "/")
	data, err := k.get(token, apiUrl+"/api/data/persons/"+personId)
	if err != nil {
		return ExternalUser{}, err
	}
	var person kitsuUser
	if err := json.Unmarshal(data, &person); err != nil {
		return ExternalUser{}, err
	}
	return ExternalUser{
		ID:    person.ID,
		Name:  person.FullName,
		Email: person.Email,
	}, nil
}

// GetProjectAssignments fetches every task for a project and returns the
// authoritative assignment state. Used by the reconciliation loop to diff
// Kitsu vs. local state on reconnect and on the 5-minute tick.
func (k *KitsuClient) GetProjectAssignments(token, apiUrl, projectId string) ([]ExternalAssignment, error) {
	apiUrl = strings.TrimSuffix(apiUrl, "/")
	data, err := k.get(token, apiUrl+"/api/data/projects/"+projectId+"/tasks")
	if err != nil {
		return nil, err
	}
	var tasks []kitsuTask
	if err := json.Unmarshal(data, &tasks); err != nil {
		return nil, err
	}
	out := make([]ExternalAssignment, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, ExternalAssignment{
			IntegrationId: "kitsu",
			ProjectId:     projectId,
			TaskId:        t.ID,
			TaskTypeId:    t.TaskTypeID,
			TaskTypeName:  t.TaskTypeName,
			PersonIds:     t.Assignees,
			Status:        t.TaskStatusID,
		})
	}
	return out, nil
}

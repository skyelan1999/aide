package server

import (
	"fmt"
	"os"
	"reflect"
	"testing"
)

func TestIssue123RejectedQueuedSteerPreservesPromptAndDisplay(t *testing.T) {
	for _, mode := range []string{"full", "nil", "unbuffered"} {
		for index := 0; index < 3; index++ {
			t.Run(fmt.Sprintf("%s/index%d", mode, index), func(t *testing.T) {
				a := testApp(t)
				s := &Session{ID: newID(), Title: "owned queue admission fixture"}
				task := &Task{ID: newID(), Status: "running", Queue: []string{"same", "middle", "same"},
					Steers: []SteerMsg{{Content: "prior", Queued: false}, {Content: "same", Queued: true}, {Content: "immediate", Queued: false}, {Content: "middle", Queued: true}, {Content: "same", Queued: true}}}
				if mode == "full" {
					task.Steer = make(chan string, 4)
					for i := 0; i < cap(task.Steer); i++ {
						task.Steer <- fmt.Sprintf("occupied%d", i)
					}
				} else if mode == "unbuffered" {
					task.Steer = make(chan string)
				}
				s.Runs = []*Task{task}
				a.sessions[s.ID] = s
				if err := a.save(s); err != nil {
					t.Fatal(err)
				}
				beforeQueue := append([]string(nil), task.Queue...)
				beforeSteers := append([]SteerMsg(nil), task.Steers...)
				path := SessionPath(a.dataPath, s.ID, sessionBucketFor(s))
				beforeDisk, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				url := fmt.Sprintf("/api/sessions/%s/runs/%s/queue/%d", s.ID, task.ID, index)
				requireStatus(t, request(a, "POST", url, map[string]string{"action": "steer"}), 429)
				if !reflect.DeepEqual(task.Queue, beforeQueue) || !reflect.DeepEqual(task.Steers, beforeSteers) {
					t.Errorf("rejected admission changed queue/display: queue%v steers%+v", task.Queue, task.Steers)
				}
				if err := a.save(s); err != nil {
					t.Fatal(err)
				}
				afterDisk, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if string(afterDisk) != string(beforeDisk) {
					t.Error("later save persisted rejected conversion")
				}
				if mode == "full" {
					for i := 0; i < 4; i++ {
						if got := <-task.Steer; got != fmt.Sprintf("occupied%d", i) {
							t.Fatal("pending channel content changed", got)
						}
					}
					if len(task.Steer) != 0 {
						t.Fatal("rejected prompt entered channel")
					}
				}
			})
		}
	}
}

// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

package async

import (
	"sync"
)

type LogicalTime int64

type Task interface {
	Key() string
	Run() (interface{}, error)
}

type taskRun struct {
	task  Task
	time  LogicalTime
	value interface{}
	err   error
	mutex sync.RWMutex
}

func (ts taskRun) Execute() {
	val, err := ts.task.Run()
	ts.mutex.Lock()
	if err != nil {
		ts.err = err
	} else {
		ts.value = val
	}
	ts.mutex.Unlock()
}

type TaskManager struct {
	tasks map[string]taskRun
	lock  sync.RWMutex
}

func (t *TaskManager) Start() error {
	t.tasks = map[string]taskRun{}
	t.lock = sync.RWMutex{}
	return nil
}

func (t *TaskManager) Stop() error {
	return nil
}

func (t *TaskManager) Run(task Task, time LogicalTime) {
	t.lock.Lock()
	runner := taskRun{
		task: task,
		time: time,
	}
	t.tasks[task.Key()] = runner
	t.lock.Unlock()
	go runner.Execute()

}

func (t *TaskManager) ConsumeResult(task Task, asOf LogicalTime) (interface{}, error, bool) {
	state, exists := t.peekState(task.Key())
	if !exists || state.time < asOf {
		t.Run(task, asOf)
		return nil, nil, false
	}
	t.lock.Lock()
	defer t.lock.Unlock()
	delete(t.tasks, task.Key())
	return state.value, state.err, true
}

func (t *TaskManager) Peek(task Task, asOf LogicalTime) Task {
	state, exists := t.peekState(task.Key())
	if !exists || state.time < asOf {
		return nil
	}
	return state.task
}

func (t *TaskManager) peekState(key string) (taskRun, bool) {
	t.lock.RLock()
	state, exists := t.tasks[key]
	t.lock.RUnlock()
	return state, exists
}

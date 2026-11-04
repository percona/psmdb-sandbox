package main

import (
	"net/http"
	"strings"
	"sync"
)

// Locks cover configuration changes and the complete lifetime of a job, not
// just request dispatch. Different environments can still run concurrently.
var environmentOperations sync.Map

func tryEnvironmentOperation(envID string) (func(), bool) {
	key := stateFile + ":" + envID
	if _, loaded := environmentOperations.LoadOrStore(key, struct{}{}); loaded {
		return nil, false
	}
	return func() { environmentOperations.Delete(key) }, true
}

func environmentInProgress(env *Environment) bool {
	return env != nil && strings.HasSuffix(env.Status, "_in_progress")
}

func environmentOperationActive(envID string) bool {
	_, active := environmentOperations.Load(stateFile + ":" + envID)
	return active
}

func operationConflict(w http.ResponseWriter) {
	jsonError(w, http.StatusConflict, "an operation is already in progress for this environment")
}

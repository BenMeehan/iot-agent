package utils

import (
	"sync"
)

// Job represents a task to be executed by a worker.
type Job struct {
	Task func()
}

// WorkerPool manages a pool of workers to execute jobs.
type WorkerPool struct {
	workers   int
	jobQueue  chan Job
	waitGroup sync.WaitGroup
}

// NewWorkerPool creates a new WorkerPool with the specified number of workers.
func NewWorkerPool(workers int) *WorkerPool {
	pool := &WorkerPool{
		workers:  workers,
		jobQueue: make(chan Job, workers),
	}

	pool.waitGroup.Add(workers)
	for i := 0; i < workers; i++ {
		go pool.worker()
	}

	return pool
}

// worker processes jobs from the jobQueue.
func (wp *WorkerPool) worker() {
	defer wp.waitGroup.Done()
	for job := range wp.jobQueue {
		job.Task()
	}
}

// Submit adds a new job to the worker pool.
func (wp *WorkerPool) Submit(task func()) {
	wp.jobQueue <- Job{Task: task}
}

// Shutdown waits for all workers to finish and then closes the worker pool.
func (wp *WorkerPool) Shutdown() {
	close(wp.jobQueue)
	wp.waitGroup.Wait()
}

package boss

import (
	"fmt"
	"io"
	"net/http"
	"errors"
)

type WorkerPool struct  {
	nextId int
	workers map[string]*Worker   //list of all workers
	queue chan 	*Worker //this queue will only hold idle workers
	//each thread of request will pick up any idle worker from this queue
	//when the task is done worker will be enqueue back to this queue

	WorkerPoolPlatform //platform specific attributes and functions
}

type WorkerPoolPlatform interface {
	NewWorker(nextId int) *Worker //return new worker struct
	CreateInstance(worker *Worker)  //create new instance in the cloud platform
	DeleteInstance(worker *Worker) //delete cloud platform instance associated with give worker struct
}

type Worker struct {
	workerId string
	workerIp string
	isIdle	 bool
	WorkerPlatform //platform specific attributes and functions
}

type WorkerPlatform interface {
	//platform specific attributes and functions
	//do not require any functions yet
}

//return number of workers in the pool
func (pool *WorkerPool) Size() int {
	return len(pool.workers)
}

//add a new worker to the pool
func (pool *WorkerPool) ScaleUp() error {
	if pool.Size() > Conf.Worker_Cap {//if pool is full
		panic(errors.New("Exceeded Maximum Number of Worker"))
	}

	nextId := pool.nextId
	pool.nextId += 1

	worker := pool.NewWorker(nextId)

	pool.workers[worker.workerId] = worker
	pool.queue <- worker
	
	go pool.CreateInstance(worker)

	return nil
}

//remove a worker from the pool
func (pool *WorkerPool) ScaleDown() {
	worker := <-pool.queue
	for worker.isIdle != true { //wait queue until an idle worker is found
		pool.queue <- worker
	}

	delete(pool.workers, worker.workerId)
	pool.DeleteInstance(worker)
}

//run lambda function
func (pool *WorkerPool) RunLambda(w http.ResponseWriter, r *http.Request)  {
	worker := <-pool.queue
	worker.isIdle = false
	if Conf.Platform == "mock" {
		s := fmt.Sprintf("hello from %s\n", worker.workerId)
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(s))
		if err != nil {
			panic(err)
		}
	} else {
		forwardTask(w, r, worker.workerIp)
	}

	worker.isIdle = true

	pool.queue <- worker
}

//return wokers' id and their status (idle/busy)
func (pool *WorkerPool) Status() []map[string]string {
	var w = []map[string]string{}

	for workerId, worker := range pool.workers {
		var output map[string]string
		if worker.isIdle {
			output = map[string]string{workerId: "idle"}
		} else {
			output = map[string]string{workerId: "busy"}
		}
		w = append(w, output)
	} 
	return w
}

//kill and delte all workers
func (pool *WorkerPool) Close() {
	for workerId, worker := range pool.workers {
		delete(pool.workers, workerId)
		pool.DeleteInstance(worker)
	}
}

// forward request to worker
func forwardTask(w http.ResponseWriter, req *http.Request, workerIp string) error {
	host := fmt.Sprintf("%s:%d", workerIp, 5000) //TODO: read from config
	req.URL.Scheme = "http"
	req.URL.Host = host
	req.Host = host
	req.RequestURI = ""

	client := http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return err
	}
	defer resp.Body.Close()

	io.Copy(w, resp.Body)

	return nil
}
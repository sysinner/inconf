// Copyright 2020 Eryx <evorui аt gmail dοt com>, All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package injob

import (
	"fmt"
	"sync"
	"time"
)

type Daemon struct {
	mu         sync.Mutex
	cmu        sync.RWMutex
	jobs       []*JobEntry
	running    bool
	conditions map[string]int64
	stop       bool
}

func NewDaemon(args ...interface{}) (*Daemon, error) {

	d := &Daemon{
		conditions: map[string]int64{},
	}

	return d, nil
}

func (it *Daemon) Commit(j *JobEntry) *JobEntry {

	it.mu.Lock()
	defer it.mu.Unlock()

	for _, v := range it.jobs {
		if v.job.Spec().Name == j.job.Spec().Name {
			v.action = ActionStart
			if j.sch != nil {
				v.sch = j.sch
			}
			return v.Commit()
		}
	}

	it.jobs = append(it.jobs, j)

	return j.Commit()
}

func (it *Daemon) conditionAllow(j *JobEntry) bool {

	if len(j.job.Spec().Conditions) == 0 {
		return true
	}

	tn := time.Now().UnixNano() / 1e6

	it.cmu.RLock()
	defer it.cmu.RUnlock()

	for c, v := range j.job.Spec().Conditions {

		t, ok := it.conditions[c]
		if !ok {
			return false
		}

		if v == -1 || t+v >= tn {
			return true
		}

		break
	}

	return false
}

func (it *Daemon) Stop() {
	it.stop = true
	time.Sleep(200e6)
}

func (it *Daemon) Start() {

	it.mu.Lock()
	if it.running {
		it.mu.Unlock()
		return
	}
	it.running = true
	it.mu.Unlock()

	tr := time.NewTicker(time.Second)
	defer tr.Stop()

	ctx := &Context{
		daemon: it,
	}

	for !it.stop {

		tn := <-tr.C
		st := scheduleTime(tn)

		for _, j := range it.jobs {

			if !it.conditionAllow(j) {
				continue
			}

			if !j.Schedule().Hit(st) {
				continue
			}

			go j.exec(ctx)
		}
	}
}

type DaemonBriefReportJob struct {
	Name    string `json:"name" toml:"name"`
	Message string `json:"message" toml:"message"`
}

type DaemonBriefReport struct {
	Jobs []*DaemonBriefReportJob `json:"jobs" toml:"jobs"`
}

func (it *Daemon) BriefReport() *DaemonBriefReport {
	rs := &DaemonBriefReport{}
	for _, j := range it.jobs {
		if j.action != ActionStart {
			continue
		}
		str := fmt.Sprintf("exec %d times", j.Status.ExecNum)
		if log := j.Status.LastLog(); log != nil {
			str += fmt.Sprintf(", last at %s in %d ms",
				time.Unix(log.Created/1e3, (log.Created%1e3)*1e6).Format("2006-01-02 15:04:05.999"),
				log.Updated-log.Created)
			if log.Status == StatusOK {
				str += ", status ok"
			} else {
				str += ", status err"
				if log.Message != "" {
					str += ": " + log.Message
				}
			}
		}
		if next := j.sch.NextTime(); next > 0 {
			str += ", next " + time.Unix(next/1e3, 0).Format("2006-01-02 15:04:05")
		}
		rs.Jobs = append(rs.Jobs, &DaemonBriefReportJob{
			Name:    j.job.Spec().Name,
			Message: str,
		})
	}
	return rs
}

func (it *Daemon) conditionSet(name string, v int64) {
	it.cmu.Lock()
	defer it.cmu.Unlock()
	it.conditions[name] = v
}

func (it *Daemon) conditionDel(name string) {
	it.cmu.Lock()
	defer it.cmu.Unlock()
	delete(it.conditions, name)
}

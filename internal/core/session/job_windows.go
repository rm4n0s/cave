package session

import (
	"os/exec"
	"sync"
	"unsafe"

	"github.com/rm4n0s/errors"
	"golang.org/x/sys/windows"
)

// The Tags of the failures to put a session's process in a job object.
const (
	TagSessionJobCreateFailed = "SessionJobCreateFailed"
	TagSessionJobLimitFailed  = "SessionJobLimitFailed"
	TagSessionJobOpenFailed   = "SessionJobOpenFailed"
	TagSessionJobAssignFailed = "SessionJobAssignFailed"
)

// processGroup is what keeps a session's processes together: a job object (docs/SPEC.md §13.2).
type processGroup struct {
	job *job
}

// job is a job object that ends every process in it when it is closed. It is safe for several
// goroutines: a signal, a hang-up and the end of the session may meet.
type job struct {
	mu     sync.Mutex
	handle windows.Handle // 0 once closed
}

// startGroup makes the job of the process that has just started and puts the process in it. The
// processes that it starts after that are in the job too.
func startGroup(cmd *exec.Cmd) (processGroup, error) {
	handle, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return processGroup{}, errors.NewErr(TagSessionJobCreateFailed, err)
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(handle, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = windows.CloseHandle(handle) // nothing is in the job yet
		return processGroup{}, errors.NewErr(TagSessionJobLimitFailed, err)
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		_ = windows.CloseHandle(handle) // as above
		return processGroup{}, errors.NewErr(TagSessionJobOpenFailed, err)
	}
	defer func() { _ = windows.CloseHandle(process) }() // the job keeps its own hold of the process
	if err := windows.AssignProcessToJobObject(handle, process); err != nil {
		_ = windows.CloseHandle(handle) // as above
		return processGroup{}, errors.NewErr(TagSessionJobAssignFailed, err)
	}
	return processGroup{job: &job{handle: handle}}, nil
}

// terminate ends every process of the job, with the exit code 1. A job that is closed already has
// nothing left to end.
func (j *job) terminate() {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.handle != 0 {
		_ = windows.TerminateJobObject(j.handle, 1) // processes that ended already are not an error to report
	}
}

// close closes the job, and Windows ends every process in it. It can be called again.
func (j *job) close() {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.handle != 0 {
		_ = windows.CloseHandle(j.handle) // closing ends the processes; there is nothing to do if it fails
		j.handle = 0
	}
}

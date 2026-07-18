//go:build windows

// Package winjob puts the current process into a Windows Job Object with
// KILL_ON_JOB_CLOSE so that all descendant processes (llama-swap, llama-server)
// are terminated by the OS whenever llama-launcher dies for any reason,
// including force kills that skip the graceful shutdown path.
//
// Without this, killing llama-launcher mid-shutdown can orphan llama-server
// processes that keep holding tens of GB of GPU/unified memory.
package winjob

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Keep the handle alive for the lifetime of the process. Closing it is the
// kill trigger, so it must never be garbage collected or closed manually.
var jobHandle windows.Handle

// SetupKillOnClose assigns the current process to a kill-on-close job object.
// Descendants spawned after this call inherit job membership automatically.
func SetupKillOnClose() error {
	if jobHandle != 0 {
		return nil
	}

	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return fmt.Errorf("CreateJobObject: %w", err)
	}

	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		windows.CloseHandle(job)
		return fmt.Errorf("SetInformationJobObject: %w", err)
	}

	if err := windows.AssignProcessToJobObject(job, windows.CurrentProcess()); err != nil {
		windows.CloseHandle(job)
		return fmt.Errorf("AssignProcessToJobObject: %w", err)
	}

	jobHandle = job
	return nil
}

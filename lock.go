package vxlan

import (
	"os"

	"github.com/alexflint/go-filemutex"
)

//Lock represents a filesystem based mutex on a whole vxlan
//this allows us to effectively serialize any accesses to an individual network's interfaces
type Lock struct {
	Name  string
	mutex *filemutex.FileMutex
}

//NewLock returns a new Lock
func NewLock(name string) (*Lock, error) {
	fm, err := filemutex.New(DefaultLockPath + string(os.PathSeparator) + "vxlan-" + name + DefaultLockExt)
	if err != nil {
		return nil, err
	}

	return &Lock{
		Name:  name,
		mutex: fm,
	}, nil
}

//Lock acquires a lock on the vxlan
func (l *Lock) Lock() {
	l.mutex.Lock()
}

//Unlock removes the lock on the vxlan
func (l *Lock) Unlock() {
	l.mutex.Unlock()
}

//Close unlocks and closes the underlying file descriptor
func (l *Lock) Close() {
	l.mutex.Close()
}

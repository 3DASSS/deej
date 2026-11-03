package notify

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"

	"git.sr.ht/~jackmordaunt/go-toast/v2"
	"github.com/google/uuid"
)

var (
	initialized = false
	appID       = getAppID()
	appGUID     = getGUID()
)

func getAppID() string {
	ex, err := os.Executable()
	if err != nil {
		return "deej"
	}
	return strings.ToLower(strings.ReplaceAll(ex, "\\", "-"))
}

// generate guid based on exe path
func getGUID() string {
	return uuid.NewSHA1(uuid.Nil, []byte(appID)).String()
}

func initalize(appIconPath, appName string) error {
	if initialized {
		return nil
	}

	// register app in registry
	// https://learn.microsoft.com/en-us/windows/apps/design/shell/tiles-and-notifications/send-local-toast-other-apps
	err := SetAppDataCustom(AppDataCustom{
		AppID:       appID,
		DisplayName: appName,
		GUID:        appGUID,
		IconPath:    appIconPath,
	})
	if err != nil {
		return err
	}

	initialized = true
	return nil
}

// The toast library caches WinRT initialization globally, although COM apartments
// belong to OS threads. Keep initialization, registration and every Push on one
// process-lifetime thread, separate from Wails' STA and audio COM threads.
var toastThread = sync.OnceValue(func() chan<- toastRequest {
	requests, _ := startToastThread()
	return requests
})

type toastRequest struct {
	run    func() error
	result chan error
}

func startToastThread() (chan<- toastRequest, <-chan struct{}) {
	requests := make(chan toastRequest)
	done := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		// Do not return a COM-initialized thread to Go's pool. The production worker
		// lives until process exit; when a test closes requests, Go discards its thread.
		defer close(done)
		for request := range requests {
			request.result <- request.run()
		}
	}()
	return requests, done
}

func Notify(title, message, appIconPath, appName string) error {
	result := make(chan error, 1)
	toastThread() <- toastRequest{
		run:    func() error { return pushToast(title, message, appIconPath, appName) },
		result: result,
	}
	return <-result
}

func pushToast(title, message, appIconPath, appName string) error {
	err := initalize(appIconPath, appName)
	if err != nil {
		return fmt.Errorf("initialize toast: %w", err)
	}

	n := toast.Notification{
		AppID:    appID,
		Title:    title,
		Body:     message,
		Duration: "short",
	}

	err = n.Push()
	if err != nil {
		return fmt.Errorf("push toast: %w", err)
	}

	return nil
}

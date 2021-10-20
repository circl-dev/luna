package support

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/kvz/logstreamer"
)

type PM struct {
	config   Config
	services []string // Specific services to run, if empty, run all from config
}

func NewPM(cfg Config) *PM {
	return &PM{
		config: cfg,
	}
}

func (pm *PM) Run(services []string, detach bool) {
	for _, s := range services {
		hasService := false
		for name := range pm.config.Services {
			if name == s {
				hasService = true
			}
		}
		if !hasService {
			fmt.Println("Could not find service " + s)
			os.Exit(1)
		}
	}
	pm.services = services
	pm.start()
	if !detach {
		waitSig()
		pm.stop()
	}
}

func (pm *PM) Stop(services []string) {
	for _, s := range services {
		hasService := false
		for name := range pm.config.Services {
			if name == s {
				hasService = true
			}
		}
		if !hasService {
			fmt.Println("Could not find service " + s)
			os.Exit(1)
		}
	}
	pm.services = services
	pm.stop()
}

func (pm *PM) start() {
	for name, svc := range pm.config.Services {
		if len(pm.services) != 0 && !StringInSlice(name, pm.services) {
			continue
		}
		svc.name = name

		if svc.Build != "" {
			pm.build(svc)
		}

		if svc.Start != "" {
			pm.spawn(svc)
		}

		if svc.Watch {
			pm.watch(svc)
		}
	}
}

func (pm *PM) stop() {
	for _, svc := range pm.config.Services {
		if len(pm.services) != 0 && !StringInSlice(svc.name, pm.services) {
			continue
		}
		pm.kill(svc, true)
	}
}

func (pm *PM) build(svc *service) {
	fmt.Println("[BUILD]", svc.name)

	cmd := MakeCmd(svc.Build, svc.Chdir)
	out, err := cmd.CombinedOutput()
	res := strings.TrimSpace(string(out))
	if err != nil {
		fmt.Println(res)
	}
}

func (pm *PM) spawn(svc *service) {
	fmt.Println("[START]", svc.name)

	svc.cmd = MakeCmd(svc.Start, svc.Chdir)
	svc.cmd.Stdout = os.Stdout
	logger := log.New(os.Stderr, "", log.Ldate|log.Ltime)
	logStreamerErr := logstreamer.NewLogstreamer(logger, svc.name+" ", true)
	svc.cmd.Stderr = logStreamerErr

	err := svc.cmd.Start()
	if err != nil {
		fmt.Println(err)
	}
}

func (pm *PM) kill(svc *service, del bool) {
	if svc.cmd.Process == nil {
		return
	}

	fmt.Println("[KILL]", svc.name)
	err := svc.cmd.Process.Kill()
	if err != nil {
		fmt.Println(err)
	}

	if del && svc.Clean != "" {
		fmt.Println("[CLEAN]", svc.Clean)

		parts := strings.Fields(svc.Clean)
		name := parts[0]
		arg := parts[1:len(parts)]

		cmd := exec.Command(name, arg...)
		cmd.Dir = svc.Chdir

		out, err := cmd.CombinedOutput()
		res := strings.TrimSpace(string(out))
		if err != nil {
			fmt.Println(res)
		}
	}
}

func (pm *PM) watch(svc *service) *Batcher {
	watcher, err := NewWatcher(1 * time.Second)
	if err != nil {
		fmt.Println(err)
	}

	var dirs []string
	getDirs := func(dir string) error {
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				fmt.Println(err)
				return err
			}
			if info.IsDir() {
				dirs = append(dirs, path)
			}
			return nil
		})
		return err
	}

	err = getDirs(svc.Chdir)
	if err != nil {
		fmt.Println(err)
	}

	svc.FoldersToWatch = append(svc.FoldersToWatch, pm.config.FoldersToWatch...)

	if len(svc.FoldersToWatch) > 0 {
		fmt.Println("[WATCH]", strings.Join(svc.FoldersToWatch, ", "))
		for _, dir := range svc.FoldersToWatch {
			err = getDirs(dir)
			if err != nil {
				fmt.Println(err)
			}
		}
	}

	for _, dir := range dirs {
		err = watcher.Add(dir)
		if err != nil {
			fmt.Println(err)
		}
	}

	go func() {
		for {
			select {
			case evs := <-watcher.Events:
				// fmt.Println("Received System Events:", evs)
				for _, ev := range evs {
					// sometimes during rm -rf operations a '"": REMOVE' is triggered, just ignore these
					if ev.Name == "" {
						continue
					}
					// if change file is actually build file, than skip it
					if strings.HasSuffix(ev.Name, filepath.Base(svc.Start)) {
						continue
					}
					// events to watch
					importantEvent := (ev.Op == fsnotify.Create || ev.Op == fsnotify.Write || ev.Op == fsnotify.Rename || ev.Op == fsnotify.Remove)
					if importantEvent {
						fmt.Println("[CHANGE]", svc.name)
						pm.kill(svc, false)
						pm.build(svc)
						pm.spawn(svc)
						break
					}
				}
			}
		}
	}()

	return watcher
}

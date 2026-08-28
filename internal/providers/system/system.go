package system

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxProcesses = 200

type Provider struct {
	mu              sync.Mutex
	previousTotal   uint64
	previousIdle    uint64
	previousUser    uint64
	previousSystem  uint64
	previousProcess map[int]uint64
}

type Snapshot struct {
	UpdatedAt time.Time `json:"updated_at"`
	CPU       CPU       `json:"cpu"`
	Memory    Memory    `json:"memory"`
	Load      Load      `json:"load"`
	Processes []Process `json:"processes"`
}

type CPU struct {
	Percent float64 `json:"percent"`
	User    float64 `json:"user_percent"`
	System  float64 `json:"system_percent"`
}

type Memory struct {
	Percent    float64 `json:"percent"`
	UsedBytes  uint64  `json:"used_bytes"`
	TotalBytes uint64  `json:"total_bytes"`
}

type Load struct {
	One     float64 `json:"one"`
	Five    float64 `json:"five"`
	Fifteen float64 `json:"fifteen"`
}

type Process struct {
	PID           int     `json:"pid"`
	Command       string  `json:"command"`
	User          string  `json:"user"`
	CPUPercent    float64 `json:"cpu_percent"`
	MemoryPercent float64 `json:"memory_percent"`
	State         string  `json:"state"`
}

func NewProvider() *Provider {
	return &Provider{previousProcess: make(map[int]uint64)}
}

func (p *Provider) Snapshot() (Snapshot, error) {
	cpu, err := p.cpu()
	if err != nil {
		return Snapshot{}, err
	}
	memory, err := readMemory()
	if err != nil {
		return Snapshot{}, err
	}
	load, err := readLoad()
	if err != nil {
		return Snapshot{}, err
	}
	processes, err := p.processes(cpu.totalDelta, memory.TotalBytes)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{
		UpdatedAt: time.Now().UTC(),
		CPU:       cpu.value,
		Memory:    memory,
		Load:      load,
		Processes: processes,
	}, nil
}

type cpuReading struct {
	value      CPU
	totalDelta uint64
}

func (p *Provider) cpu() (cpuReading, error) {
	file, err := os.Open("/proc/stat")
	if err != nil {
		return cpuReading{}, fmt.Errorf("opening /proc/stat: %w", err)
	}
	defer func() { _ = file.Close() }()
	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return cpuReading{}, fmt.Errorf("reading /proc/stat: %w", err)
		}
		return cpuReading{}, errors.New("/proc/stat has no CPU line")
	}
	fields := strings.Fields(scanner.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return cpuReading{}, errors.New("invalid /proc/stat CPU line")
	}
	values := make([]uint64, len(fields)-1)
	for index, field := range fields[1:] {
		value, parseErr := strconv.ParseUint(field, 10, 64)
		if parseErr != nil {
			return cpuReading{}, fmt.Errorf("parsing /proc/stat: %w", parseErr)
		}
		values[index] = value
	}
	var total, idle uint64
	for _, value := range values {
		total += value
	}
	idle = values[3]
	if len(values) > 4 {
		idle += values[4]
	}
	p.mu.Lock()
	totalDelta := total - p.previousTotal
	idleDelta := idle - p.previousIdle
	userDelta := values[0] - p.previousUser
	systemDelta := values[2] - p.previousSystem
	p.previousTotal = total
	p.previousIdle = idle
	p.previousUser = values[0]
	p.previousSystem = values[2]
	p.mu.Unlock()
	if totalDelta == 0 {
		return cpuReading{totalDelta: totalDelta}, nil
	}
	busy := float64(totalDelta-idleDelta) / float64(totalDelta) * 100
	user := float64(userDelta) / float64(totalDelta) * 100
	system := float64(systemDelta) / float64(totalDelta) * 100
	return cpuReading{
		value:      CPU{Percent: busy, User: user, System: system},
		totalDelta: totalDelta,
	}, nil
}

func readMemory() (Memory, error) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return Memory{}, fmt.Errorf("opening /proc/meminfo: %w", err)
	}
	defer func() { _ = file.Close() }()
	values := make(map[string]uint64)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		value, parseErr := strconv.ParseUint(fields[1], 10, 64)
		if parseErr != nil {
			return Memory{}, fmt.Errorf("parsing /proc/meminfo: %w", parseErr)
		}
		values[strings.TrimSuffix(fields[0], ":")] = value * 1024
	}
	if err := scanner.Err(); err != nil {
		return Memory{}, fmt.Errorf("reading /proc/meminfo: %w", err)
	}
	total, ok := values["MemTotal"]
	if !ok || total == 0 {
		return Memory{}, errors.New("/proc/meminfo has no MemTotal")
	}
	available := values["MemAvailable"]
	if available > total {
		available = total
	}
	used := total - available
	return Memory{Percent: float64(used) / float64(total) * 100, UsedBytes: used, TotalBytes: total}, nil
}

func readLoad() (Load, error) {
	content, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return Load{}, fmt.Errorf("reading /proc/loadavg: %w", err)
	}
	fields := strings.Fields(string(content))
	if len(fields) < 3 {
		return Load{}, errors.New("invalid /proc/loadavg")
	}
	values := make([]float64, 3)
	for index := range values {
		values[index], err = strconv.ParseFloat(fields[index], 64)
		if err != nil {
			return Load{}, fmt.Errorf("parsing /proc/loadavg: %w", err)
		}
	}
	return Load{One: values[0], Five: values[1], Fifteen: values[2]}, nil
}

func (p *Provider) processes(totalDelta uint64, totalMemory uint64) ([]Process, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, fmt.Errorf("reading /proc: %w", err)
	}
	current := make(map[int]uint64)
	processes := make([]Process, 0, len(entries))
	for _, entry := range entries {
		pid, parseErr := strconv.Atoi(entry.Name())
		if parseErr != nil || !entry.IsDir() {
			continue
		}
		stat, statErr := readProcessStat(pid)
		if statErr != nil {
			continue
		}
		current[pid] = stat.cpuTicks
		process := Process{PID: pid, Command: stat.command, State: stateName(stat.state)}
		if totalDelta > 0 {
			p.mu.Lock()
			previous, ok := p.previousProcess[pid]
			p.mu.Unlock()
			if ok && stat.cpuTicks >= previous {
				process.CPUPercent = float64(stat.cpuTicks-previous) / float64(totalDelta) * float64(runtime.NumCPU()) * 100
			}
		}
		process.User = processUser(pid)
		if totalMemory > 0 {
			if resident, residentErr := processMemory(pid); residentErr == nil {
				process.MemoryPercent = float64(resident) / float64(totalMemory) * 100
			}
		}
		processes = append(processes, process)
	}
	p.mu.Lock()
	p.previousProcess = current
	p.mu.Unlock()
	sort.Slice(processes, func(i, j int) bool { return processes[i].CPUPercent > processes[j].CPUPercent })
	if len(processes) > maxProcesses {
		processes = processes[:maxProcesses]
	}
	return processes, nil
}

type processStat struct {
	command  string
	state    byte
	cpuTicks uint64
}

func readProcessStat(pid int) (processStat, error) {
	content, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return processStat{}, err
	}
	text := string(content)
	closeParen := strings.LastIndex(text, ")")
	if closeParen < 0 || closeParen+2 >= len(text) {
		return processStat{}, errors.New("invalid process stat")
	}
	command := strings.TrimPrefix(text[:closeParen], strconv.Itoa(pid)+" (")
	fields := strings.Fields(text[closeParen+2:])
	if len(fields) < 13 {
		return processStat{}, errors.New("incomplete process stat")
	}
	userTicks, err := strconv.ParseUint(fields[11], 10, 64)
	if err != nil {
		return processStat{}, err
	}
	systemTicks, err := strconv.ParseUint(fields[12], 10, 64)
	if err != nil {
		return processStat{}, err
	}
	return processStat{command: command, state: fields[0][0], cpuTicks: userTicks + systemTicks}, nil
}

func processUser(pid int) string {
	content, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "status"))
	if err != nil {
		return "unknown"
	}
	for _, line := range strings.Split(string(content), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] != "Uid:" {
			continue
		}
		name, lookupErr := user.LookupId(fields[1])
		if lookupErr == nil {
			return name.Username
		}
		return fields[1]
	}
	return "unknown"
}

func processMemory(pid int) (uint64, error) {
	content, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "status"))
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(content), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "VmRSS:" {
			value, parseErr := strconv.ParseUint(fields[1], 10, 64)
			return value * 1024, parseErr
		}
	}
	return 0, errors.New("process has no resident memory")
}

func stateName(state byte) string {
	switch state {
	case 'R':
		return "running"
	case 'D', 'S', 'I':
		return "sleeping"
	case 'T', 't':
		return "stopped"
	case 'Z':
		return "zombie"
	default:
		return "unknown"
	}
}

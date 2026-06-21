package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/term"
)

const (
	distroName      = "eXodite"
	distroNameLower = "exodite"
	distroID        = "exodite"
)

const (
	reset  = "\033[0m"
	red    = "\033[31m"
	green  = "\033[1;32m"
	yellow = "\033[1;33m"
	cyan   = "\033[1;36m"
	purple = "\033[1;35m"
	white  = "\033[1;37m"
)

const (
	gpuNvidia  = "NVIDIA (proprietary)"
	gpuOpenSrc = "Open Source (Intel / AMD / Nouveau)"
	gpuNone    = "None (No extra drivers)"
)

type Config struct {
	Disk          string
	DiskSizeBytes uint64
	PartLayout    string
	RootSizeGB    int
	GPU           string
	Desktop       string
	Hostname      string
	Username      string
	Password      string
	RootPass      string
	Timezone      string
	Keymap        string
	Locale        string
}

func main() {
	if os.Getuid() != 0 {
		fmt.Println(red + "[!] Root privileges required." + reset)
		os.Exit(1)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println(red + "\n[!] Interrupted. Cleaning up..." + reset)
		cleanup()
		os.Exit(1)
	}()

	setupNetwork()
	printWelcome()

	cfg := gatherConfig()

	if !confirmInstall(cfg) {
		fmt.Println("\n" + cyan + "[*] Installation aborted by user." + reset)
		os.Exit(0)
	}

	if err := runInstaller(cfg); err != nil {
		fmt.Printf("\n"+red+"[!] Installation failed: %v"+reset+"\n", err)
		cleanup()
		os.Exit(1)
	}

	cleanup()
	fmt.Println(green + "\n[✓] Installation complete! Remove the USB and reboot." + reset)
}

func runInstaller(cfg Config) error {
	type step struct {
		name string
		fn   func(Config) error
	}
	steps := []step{
		{"Partitioning disk", partitionDisk},
		{"Installing base system (openSUSE Tumbleweed)", installBase},
		{"Configuring system", configure},
	}
	for _, s := range steps {
		fmt.Println(purple + "\n=== " + s.name + " ===" + reset)
		if err := s.fn(cfg); err != nil {
			return fmt.Errorf("%s: %w", s.name, err)
		}
	}
	return nil
}

func cleanup() {
	fmt.Println("\n[*] Unmounting filesystems...")
	exec.Command("umount", "/mnt/boot/efi").Run()
	exec.Command("umount", "/mnt/home").Run()

	exec.Command("umount", "/mnt/usr/local").Run()
	exec.Command("umount", "/mnt/tmp").Run()
	exec.Command("umount", "/mnt/root").Run()
	exec.Command("umount", "/mnt/opt").Run()
	exec.Command("umount", "/mnt/var").Run()
	exec.Command("umount", "/mnt/.snapshots").Run()

	exec.Command("umount", "/mnt/run").Run()
	exec.Command("umount", "/mnt/sys").Run()
	exec.Command("umount", "/mnt/proc").Run()
	exec.Command("umount", "/mnt/dev").Run()
	exec.Command("umount", "/mnt").Run()
}

func printWelcome() {
	logo := purple +
		" ███████ ██   ██  ██████  ██████  ██ ████████ ███████\n" +
		" ██       ██ ██  ██    ██ ██   ██ ██    ██    ██     \n" +
		" █████     ███   ██    ██ ██   ██ ██    ██    █████  \n" +
		" ██       ██ ██  ██    ██ ██   ██ ██    ██    ██     \n" +
		" ███████ ██   ██  ██████  ██████  ██    ██    ███████\n" + reset
	fmt.Println(logo)
	fmt.Println(white + "Welcome to the " + distroName + " Linux Installer!" + reset)
	fmt.Println("This wizard will guide you through the installation.\n")
}

func confirmInstall(cfg Config) bool {
	fmt.Println(purple + "\n=== Installation Summary ===" + reset)
	fmt.Printf("Disk:             %s\n", cfg.Disk)
	fmt.Printf("Partition layout: %s\n", cfg.PartLayout)
	if cfg.PartLayout == "split" || cfg.PartLayout == "dualboot" {
		fmt.Printf("  Root size:      %d GiB\n", cfg.RootSizeGB)
	}
	fmt.Printf("GPU driver:       %s\n", cfg.GPU)
	fmt.Printf("Desktop:          %s\n", cfg.Desktop)
	fmt.Printf("Hostname:         %s\n", cfg.Hostname)
	fmt.Printf("Username:         %s\n", cfg.Username)
	fmt.Printf("Timezone:         %s\n", cfg.Timezone)
	fmt.Printf("Locale:           %s\n", cfg.Locale)
	fmt.Printf("Keymap:           %s\n", cfg.Keymap)

	answer := prompt(yellow+"Proceed with installation? (yes/no)"+reset+" ", "no", false)
	return strings.ToLower(answer) == "yes" || strings.ToLower(answer) == "y"
}

func spinner(msg string, fn func() error) error {
	fmt.Print(cyan + "[*] " + msg + "... " + reset)
	err := fn()
	if err != nil {
		fmt.Print(red + "FAILED" + reset + "\n")
	} else {
		fmt.Print(green + "OK" + reset + "\n")
	}
	return err
}

func menuSelect(title string, options []string) string {
	fmt.Printf(purple+"  %s\n"+reset, title)
	fmt.Println()
	for i, opt := range options {
		fmt.Printf("  %d. %s\n", i+1, opt)
	}
	fmt.Println()
	for {
		answer := prompt(fmt.Sprintf("Select [1-%d]", len(options)), "", false)
		n, err := strconv.Atoi(answer)
		if err == nil && n >= 1 && n <= len(options) {
			return options[n-1]
		}
		fmt.Println(red + "  Invalid selection." + reset)
	}
}

func prompt(msg, def string, mask bool) string {
	if def != "" {
		fmt.Printf(yellow+"? "+reset+"%s ["+white+"%s"+reset+"]: ", msg, def)
	} else {
		fmt.Printf(yellow+"? "+reset+"%s: ", msg)
	}
	if mask {
		fd := int(os.Stdin.Fd())
		b, err := term.ReadPassword(fd)
		fmt.Println()
		if err != nil || len(b) == 0 {
			return def
		}
		return strings.TrimSpace(string(b))
	}
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}

func setupNetwork() {
	connected := false
	if err := exec.Command("systemctl", "start", "NetworkManager").Run(); err == nil {
		time.Sleep(2 * time.Second)
		connected = checkNetwork()
	}
	if !connected {
		if err := exec.Command("dhcpcd").Run(); err == nil {
			time.Sleep(2 * time.Second)
			connected = checkNetwork()
		}
	}
	if !connected {
		fmt.Println(red + "[!] Network not available." + reset)
		if strings.ToLower(prompt("Open nmtui to connect? (Y/n)", "y", false)) == "y" {
			exec.Command("nmtui").Run()
			if !checkNetwork() {
				fmt.Println(red + "[!] Still no network. Continuing anyway..." + reset)
			}
		}
	}
}

func checkNetwork() bool {
	return exec.Command("ping", "-c", "1", "-W", "3", "8.8.8.8").Run() == nil
}

var keymaps = []string{
	"us", "de", "uk", "fr", "es", "it", "pt", "ru", "pl", "nl", "colemak",
}

var locales = []string{
	"en_US.UTF-8", "en_GB.UTF-8", "de_DE.UTF-8", "fr_FR.UTF-8",
	"es_ES.UTF-8", "it_IT.UTF-8", "pt_PT.UTF-8", "ru_RU.UTF-8",
}

func validUsername(s string) bool {
	matched, _ := regexp.MatchString(`^[a-z_][a-z0-9_-]{0,31}$`, s)
	return matched
}

func validHostname(s string) bool {
	matched, _ := regexp.MatchString(`^[a-zA-Z0-9]([a-zA-Z0-9\-]{0,61}[a-zA-Z0-9])?$`, s)
	return matched
}

func validTimezone(s string) bool {
	_, err := os.Stat("/usr/share/zoneinfo/" + s)
	return err == nil
}

func gatherConfig() Config {
	var cfg Config

	cfg.Keymap = menuSelect("Keyboard Layout", keymaps)
	exec.Command("loadkeys", cfg.Keymap).Run()

	tz, _ := os.Readlink("/etc/localtime")
	defaultTZ := "UTC"
	if tz != "" && strings.HasPrefix(tz, "/usr/share/zoneinfo/") {
		defaultTZ = strings.TrimPrefix(tz, "/usr/share/zoneinfo/")
	}
	for {
		cfg.Timezone = prompt("Timezone", defaultTZ, false)
		if validTimezone(cfg.Timezone) {
			break
		}
		fmt.Println(red + "[!] Invalid timezone. Example: Europe/Berlin, America/New_York" + reset)
	}

	cfg.Locale = menuSelect("Locale", locales)

	out, _ := exec.Command("lsblk", "-d", "-n", "-o", "NAME,SIZE,MODEL").Output()
	type diskInfo struct {
		path      string
		size      string
		model     string
		sizeBytes uint64
	}
	var disksFound []diskInfo
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := fields[0]
		if strings.HasPrefix(name, "loop") {
			continue
		}
		d := diskInfo{
			path:  "/dev/" + name,
			size:  fields[1],
			model: strings.Join(fields[2:], " "),
		}
		sizeOut, _ := exec.Command("lsblk", "-b", "-d", "-n", "-o", "SIZE", d.path).Output()
		if b, err := strconv.ParseUint(strings.TrimSpace(string(sizeOut)), 10, 64); err == nil && b > 0 {
			d.sizeBytes = b
		}
		disksFound = append(disksFound, d)
	}
	if len(disksFound) == 0 {
		fmt.Println(red + "[!] No disks found." + reset)
		os.Exit(1)
	}

	diskOptions := make([]string, len(disksFound))
	for i, d := range disksFound {
		diskOptions[i] = fmt.Sprintf("%s  (%s %s)", d.path, d.size, d.model)
	}
	diskChoice := menuSelect("Select Installation Disk", diskOptions)
	chosenPath := strings.Fields(diskChoice)[0]
	for _, d := range disksFound {
		if d.path == chosenPath {
			cfg.Disk = d.path
			cfg.DiskSizeBytes = d.sizeBytes
			break
		}
	}

	const minDiskBytes = 20 * 1024 * 1024 * 1024
	if cfg.DiskSizeBytes > 0 && cfg.DiskSizeBytes < minDiskBytes {
		fmt.Printf(yellow+"[!] Warning: disk is only %d GiB. Minimum recommended is 20 GiB.\n"+reset, cfg.DiskSizeBytes/1024/1024/1024)
		answer := prompt("Continue anyway? (yes/no)", "no", false)
		if strings.ToLower(answer) != "yes" && strings.ToLower(answer) != "y" {
			fmt.Println(cyan + "[*] Aborted." + reset)
			os.Exit(0)
		}
	}

	layout := menuSelect("Partition Layout", []string{
		"Single partition (root only)",
		"Separate /home partition",
		"Dualboot (alongside existing OS)",
	})
	switch layout {
	case "Separate /home partition":
		cfg.PartLayout = "split"
		defRoot := "30"
		if cfg.DiskSizeBytes > 0 {
			diskGB := cfg.DiskSizeBytes / 1024 / 1024 / 1024
			if diskGB < 40 {
				defRoot = fmt.Sprintf("%d", diskGB/2)
			}
		}
		for {
			sizeStr := prompt("Root partition size in GiB", defRoot, false)
			s, err := strconv.Atoi(sizeStr)
			if err == nil && s > 0 {
				maxRoot := int(cfg.DiskSizeBytes/(1024*1024*1024)) - 1
				if s > maxRoot {
					fmt.Printf(red+"Root size too large. Max is %d GiB (disk minus 1 GiB EFI).\n"+reset, maxRoot)
					continue
				}
				cfg.RootSizeGB = s
				break
			}
			fmt.Println(red + "Invalid size, please enter a positive number." + reset)
		}
	case "Dualboot (alongside existing OS)":
		cfg.PartLayout = "dualboot"
		fmt.Println(yellow + "[!] Dualboot requires unallocated free space already on the disk." + reset)
		fmt.Println("    Use GParted or Windows Disk Management to shrink a partition first.")
		for {
			sizeStr := prompt("Root partition size in GiB", "30", false)
			s, err := strconv.Atoi(sizeStr)
			if err == nil && s > 0 {
				freeBytes := checkFreeSpace(cfg.Disk)
				freeGiB := freeBytes / (1024 * 1024 * 1024)
				if uint64(s) > freeGiB {
					fmt.Printf(red+"Not enough free space. Free: %d GiB.\n"+reset, freeGiB)
					continue
				}
				cfg.RootSizeGB = s
				break
			}
			fmt.Println(red + "Invalid size, please enter a positive number." + reset)
		}
	default:
		cfg.PartLayout = "single"
	}

	cfg.GPU = menuSelect("Graphics Driver", []string{gpuNvidia, gpuOpenSrc, gpuNone})

	cfg.Desktop = menuSelect("Desktop Environment", []string{
		"KDE Plasma", "XFCE4", "GNOME", "None (TTY only)",
	})

	fmt.Println(purple + "\n--- User Accounts ---" + reset)

	for {
		cfg.Hostname = prompt("Hostname", distroNameLower, false)
		if validHostname(cfg.Hostname) {
			break
		}
		fmt.Println(red + "[!] Invalid hostname. Use letters, digits, and hyphens only." + reset)
	}

	for {
		cfg.Username = prompt("Username", "user", false)
		if validUsername(cfg.Username) {
			break
		}
		fmt.Println(red + "[!] Invalid username. Use lowercase letters, digits, hyphens, or underscores." + reset)
	}

	for {
		cfg.Password = prompt("User password", "", true)
		if cfg.Password == "" {
			fmt.Println(red + "[!] Password cannot be empty." + reset)
			continue
		}
		confirm := prompt("Confirm user password", "", true)
		if cfg.Password == confirm {
			break
		}
		fmt.Println(red + "[!] Passwords do not match." + reset)
	}

	for {
		cfg.RootPass = prompt("Root password", "", true)
		if cfg.RootPass == "" {
			fmt.Println(red + "[!] Password cannot be empty." + reset)
			continue
		}
		confirm := prompt("Confirm root password", "", true)
		if cfg.RootPass == confirm {
			break
		}
		fmt.Println(red + "[!] Passwords do not match." + reset)
	}

	return cfg
}

func checkFreeSpace(disk string) uint64 {
	out, err := exec.Command("sgdisk", "--print", disk).Output()
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "free space") {
			continue
		}
		fields := strings.Fields(line)
		for i := 0; i < len(fields)-3; i++ {
			if fields[i] == "free" && fields[i+1] == "space" {
				raw := strings.TrimPrefix(fields[i+3], "(")
				switch {
				case strings.HasSuffix(raw, "KiB"):
					n, _ := strconv.ParseUint(strings.TrimSuffix(raw, "KiB"), 10, 64)
					return n * 1024
				case strings.HasSuffix(raw, "MiB"):
					n, _ := strconv.ParseUint(strings.TrimSuffix(raw, "MiB"), 10, 64)
					return n * 1024 * 1024
				case strings.HasSuffix(raw, "GiB"):
					n, _ := strconv.ParseUint(strings.TrimSuffix(raw, "GiB"), 10, 64)
					return n * 1024 * 1024 * 1024
				case strings.HasSuffix(raw, "TiB"):
					n, _ := strconv.ParseUint(strings.TrimSuffix(raw, "TiB"), 10, 64)
					return n * 1024 * 1024 * 1024 * 1024
				}
			}
		}
	}
	return 0
}

func listPartitions(disk string) []string {
	var parts []string
	out, err := exec.Command("lsblk", "-nlo", "NAME", disk).Output()
	if err != nil {
		return parts
	}
	base := filepath.Base(disk)
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		name := strings.TrimSpace(line)
		name = strings.TrimLeft(name, "├─└─ ")
		if name != "" && name != base {
			parts = append(parts, "/dev/"+name)
		}
	}
	return parts
}

func findNewPartitions(disk string, before []string) []string {
	after := listPartitions(disk)
	var newParts []string
	for _, ap := range after {
		exists := false
		for _, bp := range before {
			if ap == bp {
				exists = true
				break
			}
		}
		if !exists {
			newParts = append(newParts, ap)
		}
	}
	return newParts
}

func partitionDisk(cfg Config) error {
	if cfg.PartLayout == "dualboot" {
		return partitionDiskDualboot(cfg)
	}
	disk := cfg.Disk
	before := listPartitions(disk)

	if err := spinner("Wiping partition table", func() error {
		return exec.Command("sgdisk", "-Z", disk).Run()
	}); err != nil {
		return err
	}
	exec.Command("udevadm", "settle", "--timeout=10").Run()

	if cfg.PartLayout == "split" {
		if err := spinner("Creating EFI partition (1 GiB)", func() error {
			return exec.Command("sgdisk", "-n", "1:0:+1G", "-t", "1:ef00", disk).Run()
		}); err != nil {
			return err
		}
		if err := spinner(fmt.Sprintf("Creating root partition (%d GiB)", cfg.RootSizeGB), func() error {
			return exec.Command("sgdisk", "-n", "2:0:+"+strconv.Itoa(cfg.RootSizeGB)+"G", "-t", "2:8300", disk).Run()
		}); err != nil {
			return err
		}
		if err := spinner("Creating home partition (rest of disk)", func() error {
			return exec.Command("sgdisk", "-n", "3:0:0", "-t", "3:8300", disk).Run()
		}); err != nil {
			return err
		}
	} else {
		if err := spinner("Creating EFI partition (1 GiB)", func() error {
			return exec.Command("sgdisk", "-n", "1:0:+1G", "-t", "1:ef00", disk).Run()
		}); err != nil {
			return err
		}
		if err := spinner("Creating root partition (rest of disk)", func() error {
			return exec.Command("sgdisk", "-n", "2:0:0", "-t", "2:8300", disk).Run()
		}); err != nil {
			return err
		}
	}
	exec.Command("udevadm", "settle", "--timeout=10").Run()

	newParts := findNewPartitions(disk, before)
	if len(newParts) < 2 {
		return fmt.Errorf("could not detect all new partitions")
	}
	var efi, root, home string
	for _, p := range newParts {
		typ, _ := exec.Command("lsblk", "-nlo", "PARTTYPE", p).Output()
		t := strings.TrimSpace(string(typ))
		switch {
		case strings.EqualFold(t, "c12a7328-f81f-11d2-ba4b-00a0c93ec93b") || t == "EFI System":
			efi = p
		case strings.HasPrefix(t, "0fc63daf-8483-4772-8e79-3d69d8477de4") || t == "Linux filesystem":
			if root == "" {
				root = p
			} else {
				home = p
			}
		}
	}
	if efi == "" || root == "" {
		return fmt.Errorf("unable to identify EFI/root partitions")
	}

	if err := spinner("Formatting EFI (FAT32)", func() error {
		return exec.Command("mkfs.fat", "-F32", efi).Run()
	}); err != nil {
		return err
	}

	if err := spinner("Formatting root (Btrfs)", func() error {
		return exec.Command("mkfs.btrfs", "-f", root).Run()
	}); err != nil {
		return err
	}

	tmpMount := "/mnt/btrfs_tmp"
	os.MkdirAll(tmpMount, 0755)
	if err := exec.Command("mount", root, tmpMount).Run(); err != nil {
		return err
	}

	subvols := []string{"@", "@/.snapshots", "@/home", "@/var", "@/opt", "@/root", "@/tmp", "@/usr/local"}
	for _, sv := range subvols {
		full := filepath.Join(tmpMount, sv)
		if err := exec.Command("btrfs", "subvolume", "create", full).Run(); err != nil {
			exec.Command("umount", tmpMount).Run()
			return err
		}
	}
	exec.Command("chattr", "+C", filepath.Join(tmpMount, "@/var")).Run()
	exec.Command("umount", tmpMount).Run()

	if err := spinner("Mounting root subvolume", func() error {
		return exec.Command("mount", "-o", "subvol=@", root, "/mnt").Run()
	}); err != nil {
		return err
	}

	subvolsToMount := []string{".snapshots", "var", "opt", "root", "tmp", "usr/local"}
	if cfg.PartLayout != "split" {
		subvolsToMount = append(subvolsToMount, "home")
	}
	for _, sv := range subvolsToMount {
		targetDir := filepath.Join("/mnt", sv)
		os.MkdirAll(targetDir, 0755)
		exec.Command("mount", "-o", "subvol=@/"+sv, root, targetDir).Run()
	}

	os.MkdirAll("/mnt/boot/efi", 0755)
	if err := spinner("Mounting EFI", func() error {
		return exec.Command("mount", efi, "/mnt/boot/efi").Run()
	}); err != nil {
		return err
	}

	if cfg.PartLayout == "split" && home != "" {
		if err := spinner("Formatting home (ext4)", func() error {
			return exec.Command("mkfs.ext4", "-F", home).Run()
		}); err != nil {
			return err
		}
		os.MkdirAll("/mnt/home", 0755)
		if err := spinner("Mounting home", func() error {
			return exec.Command("mount", home, "/mnt/home").Run()
		}); err != nil {
			return err
		}
	}
	return nil
}

func partitionDiskDualboot(cfg Config) error {
	disk := cfg.Disk
	fmt.Println(cyan + "[*] Setting up dualboot — existing data will NOT be wiped" + reset)
	partsBefore := listPartitions(disk)

	var efiDevice string
	out, _ := exec.Command("lsblk", "-nlo", "NAME,PARTTYPE", disk).Output()
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && strings.EqualFold(fields[1], "c12a7328-f81f-11d2-ba4b-00a0c93ec93b") {
			efiDevice = "/dev/" + fields[0]
			break
		}
	}
	hasEFI := efiDevice != ""
	if !hasEFI {
		fmt.Println("[*] No EFI partition found. Creating one in free space...")
		const efiNeedBytes = uint64(512) * 1024 * 1024
		freeBytes := checkFreeSpace(disk)
		if freeBytes < efiNeedBytes {
			return fmt.Errorf("not enough free space for EFI partition: need 512 MiB but only %d MiB available", freeBytes/1024/1024)
		}
		if err := spinner("Creating EFI partition (512 MiB)", func() error {
			out, err := exec.Command("sgdisk", "-n", "0:0:+512M", "-t", "0:ef00", disk).CombinedOutput()
			if err != nil {
				return fmt.Errorf("sgdisk: %s", strings.TrimSpace(string(out)))
			}
			return nil
		}); err != nil {
			return err
		}
		exec.Command("partprobe", disk).Run()
		exec.Command("udevadm", "settle", "--timeout=10").Run()
		newEFIs := findNewPartitions(disk, partsBefore)
		if len(newEFIs) != 1 {
			return fmt.Errorf("could not identify new EFI partition")
		}
		efiDevice = newEFIs[0]
		partsBefore = listPartitions(disk)
	}
	if efiDevice == "" {
		return fmt.Errorf("could not determine EFI partition")
	}

	needBytes := uint64(cfg.RootSizeGB) * 1024 * 1024 * 1024
	freeBytes := checkFreeSpace(disk)
	if freeBytes < needBytes {
		return fmt.Errorf("not enough free space: need %d GiB but only %d GiB available", cfg.RootSizeGB, freeBytes/1024/1024/1024)
	}
	if err := spinner(fmt.Sprintf("Creating root partition (%d GiB) in free space", cfg.RootSizeGB), func() error {
		out, err := exec.Command("sgdisk", "-n", "0:0:+"+strconv.Itoa(cfg.RootSizeGB)+"G", "-t", "0:8300", disk).CombinedOutput()
		if err != nil {
			return fmt.Errorf("sgdisk: %s", strings.TrimSpace(string(out)))
		}
		return nil
	}); err != nil {
		return err
	}
	exec.Command("partprobe", disk).Run()
	exec.Command("udevadm", "settle", "--timeout=10").Run()
	newRoots := findNewPartitions(disk, partsBefore)
	if len(newRoots) != 1 {
		return fmt.Errorf("could not identify new root partition")
	}
	rootDevice := newRoots[0]

	if err := spinner("Formatting root (Btrfs)", func() error {
		return exec.Command("mkfs.btrfs", "-f", rootDevice).Run()
	}); err != nil {
		return err
	}
	if !hasEFI {
		if err := spinner("Formatting EFI (FAT32)", func() error {
			return exec.Command("mkfs.fat", "-F32", efiDevice).Run()
		}); err != nil {
			return err
		}
	}

	tmpMount := "/mnt/btrfs_tmp"
	os.MkdirAll(tmpMount, 0755)
	if err := exec.Command("mount", rootDevice, tmpMount).Run(); err != nil {
		return err
	}
	subvols := []string{"@", "@/.snapshots", "@/home", "@/var", "@/opt", "@/root", "@/tmp", "@/usr/local"}
	for _, sv := range subvols {
		full := filepath.Join(tmpMount, sv)
		if err := exec.Command("btrfs", "subvolume", "create", full).Run(); err != nil {
			exec.Command("umount", tmpMount).Run()
			return err
		}
	}
	exec.Command("chattr", "+C", filepath.Join(tmpMount, "@/var")).Run()
	exec.Command("umount", tmpMount).Run()

	if err := spinner("Mounting root subvolume", func() error {
		return exec.Command("mount", "-o", "subvol=@", rootDevice, "/mnt").Run()
	}); err != nil {
		return err
	}

	subvolsToMount := []string{".snapshots", "var", "opt", "root", "tmp", "usr/local"}
	if cfg.PartLayout != "split" {
		subvolsToMount = append(subvolsToMount, "home")
	}
	for _, sv := range subvolsToMount {
		targetDir := filepath.Join("/mnt", sv)
		os.MkdirAll(targetDir, 0755)
		exec.Command("mount", "-o", "subvol=@/"+sv, rootDevice, targetDir).Run()
	}

	os.MkdirAll("/mnt/boot/efi", 0755)
	if err := spinner("Mounting EFI", func() error {
		return exec.Command("mount", efiDevice, "/mnt/boot/efi").Run()
	}); err != nil {
		return err
	}

	fmt.Println(green + "[✓] Dualboot partitions ready." + reset)
	return nil
}

func installBase(cfg Config) error {
	repoOSS := "http://download.opensuse.org/tumbleweed/repo/oss/"
	repoNonOSS := "http://download.opensuse.org/tumbleweed/repo/non-oss/"

	if err := spinner("Adding OSS repository", func() error {
		return exec.Command("zypper", "--root", "/mnt", "--gpg-auto-import-keys", "ar", "-f", repoOSS, "repo-oss").Run()
	}); err != nil {
		return err
	}
	if err := spinner("Adding Non-OSS repository", func() error {
		return exec.Command("zypper", "--root", "/mnt", "--gpg-auto-import-keys", "ar", "-f", repoNonOSS, "repo-non-oss").Run()
	}); err != nil {
		return err
	}
	if err := spinner("Refreshing repositories", func() error {
		return exec.Command("zypper", "--root", "/mnt", "--gpg-auto-import-keys", "refresh").Run()
	}); err != nil {
		return err
	}

	packages := []string{
		"patterns-base-minimal_base",
		"patterns-base-enhanced_base",
		"kernel-default",
		"grub2-efi",
		"grub2",
		"NetworkManager",
		"sudo",
	}

	if cfg.GPU == gpuNvidia {
		packages = append(packages, "kernel-default-devel", "nvidia-driver-G06-kmp-default", "nvidia-gl-G06")
	} else if cfg.GPU == gpuOpenSrc {
		packages = append(packages, "kernel-firmware")
	}

	switch cfg.Desktop {
	case "KDE Plasma":
		packages = append(packages, "patterns-kde-plasma")
	case "XFCE4":
		packages = append(packages, "patterns-xfce")
	case "GNOME":
		packages = append(packages, "patterns-gnome")
	}

	args := []string{"--root", "/mnt", "--gpg-auto-import-keys", "install", "-y"}
	args = append(args, packages...)

	if err := spinner("Installing system packages", func() error {
		cmd := exec.Command("zypper", args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}); err != nil {
		return err
	}

	return nil
}

func writeFstab(cfg Config) error {
	var lines []string

	out, err := exec.Command("lsblk", "-P", "-o", "NAME,MOUNTPOINT,FSTYPE,UUID").Output()
	if err != nil {
		return err
	}

	re := regexp.MustCompile(`([A-Z]+)="([^"]*)"`)

	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		matches := re.FindAllStringSubmatch(line, -1)
		fields := make(map[string]string)
		for _, m := range matches {
			fields[m[1]] = m[2]
		}

		mount := fields["MOUNTPOINT"]
		fstype := fields["FSTYPE"]
		uuid := fields["UUID"]

		if uuid == "" {
			continue
		}

		if mount != "/mnt" && mount != "/mnt/boot/efi" && mount != "/mnt/home" {
			continue
		}

		targetMount := strings.TrimPrefix(mount, "/mnt")
		if targetMount == "" {
			targetMount = "/"
		}

		if fstype == "btrfs" && targetMount != "/" {
			continue
		}

		if fstype == "btrfs" && targetMount == "/" {
			lines = append(lines, fmt.Sprintf("UUID=%s / btrfs defaults,subvol=@ 0 0", uuid))

			subvols := map[string]string{
				"/.snapshots": "@/.snapshots",
				"/var":         "@/var",
				"/opt":         "@/opt",
				"/root":        "@/root",
				"/tmp":         "@/tmp",
				"/usr/local":   "@/usr/local",
			}

			if cfg.PartLayout != "split" {
				subvols["/home"] = "@/home"
			}

			for targetPath, subvolName := range subvols {
				lines = append(lines, fmt.Sprintf("UUID=%s %s btrfs defaults,subvol=%s 0 0", uuid, targetPath, subvolName))
			}
		} else {
			lines = append(lines, fmt.Sprintf("UUID=%s %s %s defaults 0 2", uuid, targetMount, fstype))
		}
	}

	if len(lines) == 0 {
		return fmt.Errorf("no mount points found for fstab")
	}

	return os.WriteFile("/mnt/etc/fstab", []byte(strings.Join(lines, "\n")+"\n"), 0644)
}

func configure(cfg Config) error {
	os.MkdirAll("/mnt/proc", 0755)
	os.MkdirAll("/mnt/sys", 0755)
	os.MkdirAll("/mnt/dev", 0755)
	os.MkdirAll("/mnt/run", 0755)

	mountCmd := func(source, target, fstype string, flags uintptr, data string) error {
		return syscall.Mount(source, target, fstype, flags, data)
	}
	if err := mountCmd("proc", "/mnt/proc", "proc", 0, ""); err != nil {
		return err
	}
	if err := mountCmd("/sys", "/mnt/sys", "", syscall.MS_BIND, ""); err != nil {
		return err
	}
	if err := mountCmd("/dev", "/mnt/dev", "", syscall.MS_BIND, ""); err != nil {
		return err
	}
	if err := mountCmd("tmpfs", "/mnt/run", "tmpfs", 0, "mode=0755"); err != nil {
		return err
	}
	defer func() {
		syscall.Unmount("/mnt/run", 0)
		syscall.Unmount("/mnt/dev", 0)
		syscall.Unmount("/mnt/sys", 0)
		syscall.Unmount("/mnt/proc", 0)
	}()

	exec.Command("cp", "/etc/resolv.conf", "/mnt/etc/").Run()

	if err := writeFstab(cfg); err != nil {
		return err
	}

	script := "#!/bin/bash\nset -e\n"
	script += fmt.Sprintf("echo '%s' > /etc/hostname\n", cfg.Hostname)
	script += fmt.Sprintf("ln -sf /usr/share/zoneinfo/%s /etc/localtime\n", cfg.Timezone)
	script += fmt.Sprintf("echo '%s' > /etc/timezone\n", cfg.Timezone)
	script += fmt.Sprintf("echo 'LANG=%s' > /etc/locale.conf\n", cfg.Locale)
	script += fmt.Sprintf("echo 'KEYMAP=%s' > /etc/vconsole.conf\n", cfg.Keymap)
	script += fmt.Sprintf("cat > /etc/os-release << 'EOF'\nNAME=\"%s Linux\"\nID=%s\nPRETTY_NAME=\"%s Linux\"\nEOF\n", distroName, distroID, distroName)
	script += fmt.Sprintf("useradd -m -G wheel,users -s /bin/bash '%s'\n", cfg.Username)
	script += fmt.Sprintf("echo 'root:%s' | chpasswd\n", cfg.RootPass)
	script += fmt.Sprintf("echo '%s:%s' | chpasswd\n", cfg.Username, cfg.Password)
	script += `echo '%wheel ALL=(ALL:ALL) ALL' > /etc/sudoers.d/10-wheel
chmod 440 /etc/sudoers.d/10-wheel
`
	script += "mkinitrd\n"
	script += `grub2-install --target=x86_64-efi --efi-directory=/boot/efi --bootloader-id=` + distroName + "\n"
	script += `grub2-mkconfig -o /boot/grub2/grub.cfg` + "\n"
	script += "systemctl enable NetworkManager\n"
	switch cfg.Desktop {
	case "KDE Plasma":
		script += "systemctl enable sddm\n"
	case "XFCE4":
		script += "systemctl enable lightdm\n"
	case "GNOME":
		script += "systemctl enable gdm\n"
	}

	scriptPath := "/mnt/setup.sh"
	if err := os.WriteFile(scriptPath, []byte(script), 0700); err != nil {
		return err
	}
	defer os.Remove(scriptPath)

	fmt.Println(cyan + "[*] Running chroot configuration..." + reset)
	cmd := exec.Command("chroot", "/mnt", "/bin/bash", "/setup.sh")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}

	return nil
}

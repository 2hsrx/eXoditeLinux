

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
	gpuNone    = "None (just no drivers )"
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

type SubvolEntry struct {
	Path string
	Name string
}

func main() {
	if os.Getuid() != 0 {
		fmt.Println(red + " Root required." + reset)
		os.Exit(1)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println(red + "why did u cancel gng well cleaning up system now" + reset)
		cleanup()
		os.Exit(1)
	}()

	setupNetwork()
	printWelcome()

	cfg := gatherConfig()

	if !confirmInstall(cfg) {
		fmt.Println("\n" + cyan + "why tf did u cancel" + reset)
		os.Exit(0)
	}

	if err := runInstaller(cfg); err != nil {
		fmt.Printf("\n"+red+" Installation failed how bro: %v"+reset+"\n", err)
		cleanup()
		os.Exit(1)
	}

	cleanup()
	fmt.Println(green + "\n[✓] done installing reboot and pull out usb." + reset)
}

func runInstaller(cfg Config) error {
	type step struct {
		name string
		fn   func(Config) error
	}
	steps := []step{
		{"Partitioning target disk", partitionDisk},
		{"Bootstrapping system packages (openSUSE Tumbleweed)", installBase},
		{"Executing some shit", configure},
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
	fmt.Println("\n[*] Safely unmounting target filesystems")
	mountpoints := []string{
		"/mnt/boot/efi", "/mnt/home", "/mnt/usr/local", "/mnt/tmp",
		"/mnt/root", "/mnt/opt", "/mnt/var", "/mnt/.snapshots",
		"/mnt/run", "/mnt/sys", "/mnt/proc", "/mnt/dev", "/mnt",
	}
	for _, mp := range mountpoints {
		exec.Command("umount", "-l", mp).Run()
	}
}

func printWelcome() {
	logo := purple +
		" ███████ ██   ██  ██████  ██████  ██ ████████ ███████\n" +
		" ██      ██ ██  ██    ██ ██    ██ ██    ██    ██     \n" +
		" █████   ███    ██    ██ ██    ██ ██    ██    █████  \n" +
		" ██      ██ ██  ██    ██ ██    ██ ██    ██    ██     \n" +
		" ███████ ██   ██  ██████  ██████  ██    ██    ███████\n" + reset
	fmt.Println(logo)
	fmt.Println(white + "Welcome to the " + distroName + " Installer" + reset)
	fmt.Println("Follow the interactive prompts below to prepare your build.\n")
}

func confirmInstall(cfg Config) bool {
	fmt.Println(purple + "\n=== Installation Summary ===" + reset)
	fmt.Printf("Target Disk:      %s\n", cfg.Disk)
	fmt.Printf("Partition Layout: %s\n", cfg.PartLayout)
	if cfg.PartLayout == "split" || cfg.PartLayout == "dualboot" {
		fmt.Printf("  Root Size:      %d GiB\n", cfg.RootSizeGB)
	}
	fmt.Printf("GPU Driver:       %s\n", cfg.GPU)
	fmt.Printf("Desktop Env:      %s\n", cfg.Desktop)
	fmt.Printf("Target Hostname:  %s\n", cfg.Hostname)
	fmt.Printf("Primary User:     %s\n", cfg.Username)
	fmt.Printf("Timezone:         %s\n", cfg.Timezone)
	fmt.Printf("System Locale:    %s\n", cfg.Locale)
	fmt.Printf("Keymap Layout:    %s\n", cfg.Keymap)

	fmt.Println(red + "\nWARNING: Continuing may write formatting instructions to disk." + reset)
	answer := prompt(yellow+"Proceed with configuration deployment? (yes/no)"+reset+" ", "no", false)
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
	for i, opt := range options {
		fmt.Printf("  %d. %s\n", i+1, opt)
	}
	fmt.Println()
	for {
		answer := prompt(fmt.Sprintf("Select selection item [1-%d]", len(options)), "", false)
		n, err := strconv.Atoi(answer)
		if err == nil && n >= 1 && n <= len(options) {
			return options[n-1]
		}
		fmt.Println(red + "  Invalid numeric selection index." + reset)
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
		fmt.Println(red + "[!] Active network bridge not discovered." + reset)
		if strings.ToLower(prompt("Launch interactive network interface (nmtui)? (Y/n)", "y", false)) == "y" {
			exec.Command("nmtui").Run()
			if !checkNetwork() {
				fmt.Println(red + "[!] Network validation timed out. Continuing with offline tasks..." + reset)
			}
		}
	}
}

func checkNetwork() bool {
	return exec.Command("ping", "-c", "1", "-W", "3", "8.8.8.8").Run() == nil
}

var keymaps = []string{"us", "de", "uk", "fr", "es", "it", "pt", "ru", "pl", "nl", "colemak"}
var locales = []string{"en_US.UTF-8", "en_GB.UTF-8", "de_DE.UTF-8", "fr_FR.UTF-8", "es_ES.UTF-8"}

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

	cfg.Keymap = menuSelect("Select Keyboard Layout Map", keymaps)
	exec.Command("loadkeys", cfg.Keymap).Run()

	tz, _ := os.Readlink("/etc/localtime")
	defaultTZ := "UTC"
	if tz != "" && strings.HasPrefix(tz, "/usr/share/zoneinfo/") {
		defaultTZ = strings.TrimPrefix(tz, "/usr/share/zoneinfo/")
	}
	for {
		cfg.Timezone = prompt("System Timezone string", defaultTZ, false)
		if validTimezone(cfg.Timezone) {
			break
		}
		fmt.Println(red + "[!] Timezone missing from zoneinfo definitions (e.g., Europe/Berlin)." + reset)
	}

	cfg.Locale = menuSelect("System Target Locale", locales)

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
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "airoot") {
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
		fmt.Println(red + "[!] No block storage targets localized." + reset)
		os.Exit(1)
	}

	diskOptions := make([]string, len(disksFound))
	for i, d := range disksFound {
		diskOptions[i] = fmt.Sprintf("%s  (%s %s)", d.path, d.size, d.model)
	}
	diskChoice := menuSelect("Target Installation Drive Node", diskOptions)
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
		fmt.Printf(yellow+"[!] Warning: Storage limit lower than recommended space requirements (%d GiB).\n"+reset, cfg.DiskSizeBytes/1024/1024/1024)
		if strings.ToLower(prompt("Override workspace boundaries? (yes/no)", "no", false)) != "yes" {
			os.Exit(0)
		}
	}

	layout := menuSelect("Partition Topology Strategy", []string{
		"Single partition (Unified root filesystem)",
		"Separate /home volume split",
		"Dualboot setup (Alongside alternative operating system files)",
	})
	switch layout {
	case "Separate /home volume split":
		cfg.PartLayout = "split"
		defRoot := "30"
		for {
			sizeStr := prompt("Root mapping volume dimension (GiB)", defRoot, false)
			s, err := strconv.Atoi(sizeStr)
			if err == nil && s > 0 {
				maxRoot := int(cfg.DiskSizeBytes/(1024*1024*1024)) - 2
				if s > maxRoot {
					fmt.Printf(red+"Specified dimension breaks boundaries. Maximum safe partition: %d GiB.\n"+reset, maxRoot)
					continue
				}
				cfg.RootSizeGB = s
				break
			}
			fmt.Println(red + "[!] Expected positive integer notation." + reset)
		}
	case "Dualboot setup (Alongside alternative operating system files)":
		cfg.PartLayout = "dualboot"
		fmt.Println(yellow + "[!] Dualboot configurations map to existing unallocated disk slots." + reset)
		for {
			sizeStr := prompt("Target root dimension space allocation (GiB)", "30", false)
			s, err := strconv.Atoi(sizeStr)
			if err == nil && s > 0 {
				freeBytes := checkFreeSpace(cfg.Disk)
				freeGiB := freeBytes / (1024 * 1024 * 1024)
				if uint64(s) > freeGiB {
					fmt.Printf(red+"Space boundary saturated. Free space verified: %d GiB.\n"+reset, freeGiB)
					continue
				}
				cfg.RootSizeGB = s
				break
			}
			fmt.Println(red + "[!] Expected positive integer notation." + reset)
		}
	default:
		cfg.PartLayout = "single"
	}

	cfg.GPU = menuSelect("Display Driver Target Stack", []string{gpuNvidia, gpuOpenSrc, gpuNone})
	cfg.Desktop = menuSelect("Default User Interface Session", []string{
		"KDE Plasma", "XFCE4", "GNOME", "Hyprland", "None (TTY only)",
	})

	fmt.Println(purple + "\n--- Authentication Setup ---" + reset)
	for {
		cfg.Hostname = prompt("System Domain Hostname", distroNameLower, false)
		if validHostname(cfg.Hostname) {
			break
		}
		fmt.Println(red + "[!] Invalid characters found. Use clean alphanumeric symbols." + reset)
	}
	for {
		cfg.Username = prompt("Default Account User ID Name", "user", false)
		if validUsername(cfg.Username) {
			break
		}
		fmt.Println(red + "[!] Identity tags must comply with lowercase POSIX name rules." + reset)
	}
	for {
		cfg.Password = prompt("User Account Security Phrase", "", true)
		if cfg.Password == "" {
			fmt.Println(red + "[!] Blank values unaccepted." + reset)
			continue
		}
		if cfg.Password == prompt("Confirm matching phrase entry", "", true) {
			break
		}
		fmt.Println(red + "[!] Verification mismatch." + reset)
	}
	for {
		cfg.RootPass = prompt("Superuser (root) Security Phrase", "", true)
		if cfg.RootPass == "" {
			fmt.Println(red + "[!] Blank values unaccepted." + reset)
			continue
		}
		if cfg.RootPass == prompt("Confirm matching phrase entry", "", true) {
			break
		}
		fmt.Println(red + "[!] Verification mismatch." + reset)
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
				var multiplier uint64 = 1
				switch {
				case strings.HasSuffix(raw, "KiB"):
					multiplier = 1024
					raw = strings.TrimSuffix(raw, "KiB")
				case strings.HasSuffix(raw, "MiB"):
					multiplier = 1024 * 1024
					raw = strings.TrimSuffix(raw, "MiB")
				case strings.HasSuffix(raw, "GiB"):
					multiplier = 1024 * 1024 * 1024
					raw = strings.TrimSuffix(raw, "GiB")
				case strings.HasSuffix(raw, "TiB"):
					multiplier = 1024 * 1024 * 1024 * 1024
					raw = strings.TrimSuffix(raw, "TiB")
				}
				n, _ := strconv.ParseUint(raw, 10, 64)
				return n * multiplier
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

func createBtrfsSubvolumes(mountPoint string) error {
	os.MkdirAll(filepath.Join(mountPoint, "@/usr"), 0755)
	subvols := []string{"@", "@/.snapshots", "@/home", "@/var", "@/opt", "@/root", "@/tmp", "@/usr/local"}
	for _, sv := range subvols {
		full := filepath.Join(mountPoint, sv)
		out, err := exec.Command("btrfs", "subvolume", "create", full).CombinedOutput()
		if err != nil {
			return fmt.Errorf("btrfs subvolume execution failure %s: %s", sv, strings.TrimSpace(string(out)))
		}
	}
	exec.Command("chattr", "+C", filepath.Join(mountPoint, "@/var")).Run()
	return nil
}

func mountBtrfsSubvolumes(rootDevice string, cfg Config) {
	subvolsToMount := []string{".snapshots", "var", "opt", "root", "tmp", "usr/local"}
	if cfg.PartLayout != "split" {
		subvolsToMount = append(subvolsToMount, "home")
	}
	for _, sv := range subvolsToMount {
		targetDir := filepath.Join("/mnt", sv)
		os.MkdirAll(targetDir, 0755)
		exec.Command("mount", "-o", "subvol=@/"+sv, rootDevice, targetDir).Run()
	}
}

func partitionDisk(cfg Config) error {
	if cfg.PartLayout == "dualboot" {
		return partitionDiskDualboot(cfg)
	}
	disk := cfg.Disk
	before := listPartitions(disk)

	if err := spinner("Wiping existing drive headers", func() error {
		out, err := exec.Command("sgdisk", "-Z", disk).CombinedOutput()
		if err != nil {
			return fmt.Errorf("sgdisk error: %s", strings.TrimSpace(string(out)))
		}
		return nil
	}); err != nil {
		return err
	}
	exec.Command("udevadm", "settle", "--timeout=10").Run()

	if cfg.PartLayout == "split" {
		if err := spinner("Writing structural EFI table row (1 GiB)", func() error {
			_, err := exec.Command("sgdisk", "-n", "1:0:+1G", "-t", "1:ef00", disk).CombinedOutput()
			return err
		}); err != nil {
			return err
		}
		if err := spinner(fmt.Sprintf("Writing operational root table row (%d GiB)", cfg.RootSizeGB), func() error {
			_, err := exec.Command("sgdisk", "-n", "2:0:+"+strconv.Itoa(cfg.RootSizeGB)+"G", "-t", "2:8300", disk).CombinedOutput()
			return err
		}); err != nil {
			return err
		}
		if err := spinner("Writing home directory partition table row", func() error {
			_, err := exec.Command("sgdisk", "-n", "3:0:0", "-t", "3:8300", disk).CombinedOutput()
			return err
		}); err != nil {
			return err
		}
	} else {
		if err := spinner("Writing structural EFI table row (1 GiB)", func() error {
			_, err := exec.Command("sgdisk", "-n", "1:0:+1G", "-t", "1:ef00", disk).CombinedOutput()
			return err
		}); err != nil {
			return err
		}
		if err := spinner("Writing complete root workspace tracking layout", func() error {
			_, err := exec.Command("sgdisk", "-n", "2:0:0", "-t", "2:8300", disk).CombinedOutput()
			return err
		}); err != nil {
			return err
		}
	}
	exec.Command("udevadm", "settle", "--timeout=10").Run()

	newParts := findNewPartitions(disk, before)
	if len(newParts) < 2 {
		return fmt.Errorf("could not detect all new partitions, found: %v", newParts)
	}

	var efi, root, home string
	for _, p := range newParts {
		if strings.HasSuffix(p, "1") || strings.HasSuffix(p, "p1") {
			efi = p
		} else if strings.HasSuffix(p, "2") || strings.HasSuffix(p, "p2") {
			root = p
		} else if strings.HasSuffix(p, "3") || strings.HasSuffix(p, "p3") {
			home = p
		}
	}

	if efi == "" || root == "" {
		return fmt.Errorf("unable to identify EFI/root partitions. EFI=%s Root=%s Parts=%v", efi, root, newParts)
	}

	// Verify device nodes actually exist before proceeding
	checkPaths := []string{efi, root}
	if home != "" {
		checkPaths = append(checkPaths, home)
	}
	for _, p := range checkPaths {
		if _, err := os.Stat(p); os.IsNotExist(err) {
			return fmt.Errorf("device node %s not found by kernel", p)
		}
	}

	if err := spinner("Formatting targeted EFI structure (FAT32)", func() error {
		_, err := exec.Command("mkfs.fat", "-F32", efi).CombinedOutput()
		return err
	}); err != nil {
		return err
	}

	if err := spinner("Formatting target block node (Btrfs)", func() error {
		_, err := exec.Command("mkfs.btrfs", "-f", root).CombinedOutput()
		return err
	}); err != nil {
		return err
	}

	tmpMount := "/mnt/btrfs_tmp"
	os.MkdirAll(tmpMount, 0755)
	if err := exec.Command("mount", root, tmpMount).Run(); err != nil {
		return fmt.Errorf("btrfs context access breakdown: %w", err)
	}

	if err := createBtrfsSubvolumes(tmpMount); err != nil {
		exec.Command("umount", "-l", tmpMount).Run()
		return err
	}
	exec.Command("umount", "-l", tmpMount).Run()

	if err := spinner("Mounting default root subvolume tree", func() error {
		_, err := exec.Command("mount", "-o", "subvol=@", root, "/mnt").CombinedOutput()
		return err
	}); err != nil {
		return err
	}

	mountBtrfsSubvolumes(root, cfg)

	os.MkdirAll("/mnt/boot/efi", 0755)
	if err := spinner("Staging target EFI architecture link", func() error {
		_, err := exec.Command("mount", efi, "/mnt/boot/efi").CombinedOutput()
		return err
	}); err != nil {
		return err
	}

	if cfg.PartLayout == "split" && home != "" {
		if err := spinner("Formatting split storage zone (ext4)", func() error {
			_, err := exec.Command("mkfs.ext4", "-F", home).CombinedOutput()
			return err
		}); err != nil {
			return err
		}
		os.MkdirAll("/mnt/home", 0755)
		if err := spinner("Staging operational user interface layout link", func() error {
			_, err := exec.Command("mount", home, "/mnt/home").CombinedOutput()
			return err
		}); err != nil {
			return err
		}
	}
	return nil
}

func partitionDiskDualboot(cfg Config) error {
	disk := cfg.Disk
	fmt.Println(cyan + "[*] Staging side-by-side configurations — existing parameters remain untouched" + reset)
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
		if err := spinner("Generating standalone boot system tracking slice (512 MiB)", func() error {
			_, err := exec.Command("sgdisk", "-n", "0:0:+512M", "-t", "0:ef00", disk).CombinedOutput()
			return err
		}); err != nil {
			return err
		}
		exec.Command("partprobe", disk).Run()
		exec.Command("udevadm", "settle", "--timeout=10").Run()
		newEFIs := findNewPartitions(disk, partsBefore)
		if len(newEFIs) != 1 {
			return fmt.Errorf("failed tracing standalone boot slice target link")
		}
		efiDevice = newEFIs[0]
		partsBefore = listPartitions(disk)
	}

	if err := spinner("Allotting dualboot sub-allocation space structure", func() error {
		_, err := exec.Command("sgdisk", "-n", "0:0:+"+strconv.Itoa(cfg.RootSizeGB)+"G", "-t", "0:8300", disk).CombinedOutput()
		return err
	}); err != nil {
		return err
	}
	exec.Command("partprobe", disk).Run()
	exec.Command("udevadm", "settle", "--timeout=10").Run()
	newRoots := findNewPartitions(disk, partsBefore)
	if len(newRoots) != 1 {
		return fmt.Errorf("error indexing targeted application space node")
	}
	rootDevice := newRoots[0]

	if err := spinner("Formatting layout framework target (Btrfs)", func() error {
		_, err := exec.Command("mkfs.btrfs", "-f", rootDevice).CombinedOutput()
		return err
	}); err != nil {
		return err
	}
	if !hasEFI {
		if err := spinner("Formatting active boot file index directory (FAT32)", func() error {
			_, err := exec.Command("mkfs.fat", "-F32", efiDevice).CombinedOutput()
			return err
		}); err != nil {
			return err
		}
	}

	tmpMount := "/mnt/btrfs_tmp"
	os.MkdirAll(tmpMount, 0755)
	if err := exec.Command("mount", rootDevice, tmpMount).Run(); err != nil {
		return fmt.Errorf("failed mapping dualboot btrfs structure: %w", err)
	}
	if err := createBtrfsSubvolumes(tmpMount); err != nil {
		exec.Command("umount", "-l", tmpMount).Run()
		return err
	}
	exec.Command("umount", "-l", tmpMount).Run()

	if err := exec.Command("mount", "-o", "subvol=@", rootDevice, "/mnt").Run(); err != nil {
		return fmt.Errorf("failed mounting subvol=@ root: %w", err)
	}
	mountBtrfsSubvolumes(rootDevice, cfg)
	os.MkdirAll("/mnt/boot/efi", 0755)
	if err := exec.Command("mount", efiDevice, "/mnt/boot/efi").Run(); err != nil {
		return fmt.Errorf("failed mapping EFI structure: %w", err)
	}

	return nil
}

func getUUID(path string) (string, error) {
	out, err := exec.Command("lsblk", "-d", "-no", "UUID", path).Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func writeFstab(cfg Config) error {
	rootUUID, err := getUUID("/mnt")
	if err != nil || rootUUID == "" {
		return fmt.Errorf("failed parsing target root matrix index token")
	}
	efiUUID, err := getUUID("/mnt/boot/efi")
	if err != nil || efiUUID == "" {
		return fmt.Errorf("failed locating architecture partition mapping key")
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("UUID=%s / btrfs defaults,subvol=@ 0 0", rootUUID))

	subvols := []SubvolEntry{
		{"/.snapshots", "@/.snapshots"}, {"/var", "@/var"}, {"/opt", "@/opt"},
		{"/root", "@/root"}, {"/tmp", "@/tmp"}, {"/usr/local", "@/usr/local"},
	}
	if cfg.PartLayout != "split" {
		subvols = append(subvols, SubvolEntry{"/home", "@/home"})
	}
	for _, sv := range subvols {
		lines = append(lines, fmt.Sprintf("UUID=%s %s btrfs defaults,subvol=%s 0 0", rootUUID, sv.Path, sv.Name))
	}

	lines = append(lines, fmt.Sprintf("UUID=%s /boot/efi vfat defaults 0 2", efiUUID))

	if cfg.PartLayout == "split" {
		homeUUID, err := getUUID("/mnt/home")
		if err != nil || homeUUID == "" {
			return fmt.Errorf("failed establishing dynamic user directory partition key")
		}
		lines = append(lines, fmt.Sprintf("UUID=%s /home ext4 defaults 0 2", homeUUID))
	}

	return os.WriteFile("/mnt/etc/fstab", []byte(strings.Join(lines, "\n")+"\n"), 0644)
}

func installBase(cfg Config) error {
	repoOSS := "http://download.opensuse.org/tumbleweed/repo/oss/"
	repoNonOSS := "http://download.opensuse.org/tumbleweed/repo/non-oss/"

	if err := spinner("Adding core openSUSE OSS repository", func() error {
		out, err := exec.Command("zypper", "--root", "/mnt", "--gpg-auto-import-keys", "ar", "-f", repoOSS, "repo-oss").CombinedOutput()
		if err != nil {
			return fmt.Errorf("zypper oss failed: %s", strings.TrimSpace(string(out)))
		}
		return nil
	}); err != nil {
		return err
	}

	if err := spinner("Adding openSUSE Non-OSS repository", func() error {
		out, err := exec.Command("zypper", "--root", "/mnt", "--gpg-auto-import-keys", "ar", "-f", repoNonOSS, "repo-non-oss").CombinedOutput()
		if err != nil {
			return fmt.Errorf("zypper non-oss failed: %s", strings.TrimSpace(string(out)))
		}
		return nil
	}); err != nil {
		return err
	}

	if err := spinner("Syncing distribution package tracking indexes", func() error {
		out, err := exec.Command("zypper", "--root", "/mnt", "--gpg-auto-import-keys", "refresh").CombinedOutput()
		if err != nil {
			return fmt.Errorf("zypper refresh failed: %s", strings.TrimSpace(string(out)))
		}
		return nil
	}); err != nil {
		return err
	}

	packages := []string{
		"patterns-base-minimal_base", "patterns-base-enhanced_base",
		"kernel-default", "grub2-efi", "grub2", "NetworkManager", "sudo",
	}

	if cfg.PartLayout == "dualboot" {
		packages = append(packages, "os-prober")
	}
	if cfg.GPU == gpuNvidia {
		packages = append(packages, "kernel-default-devel", "nvidia-driver-G06-kmp-default", "nvidia-gl-G06")
	} else if cfg.GPU == gpuOpenSrc {
		packages = append(packages, "kernel-firmware")
	}

	switch cfg.Desktop {
	case "KDE Plasma":
		packages = append(packages, "patterns-kde-kde_plasma")
	case "XFCE4":
		packages = append(packages, "patterns-xfce-xfce")
	case "GNOME":
		packages = append(packages, "patterns-gnome-gnome")
	case "Hyprland":
		packages = append(packages, "hyprland", "sddm")
	}

	args := []string{"--root", "/mnt", "--gpg-auto-import-keys", "install", "-y"}
	args = append(args, packages...)

	fmt.Println(cyan + "[*] Streaming live installation packages via Zypper..." + reset)
	cmd := exec.Command("zypper", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func configure(cfg Config) error {
	os.MkdirAll("/mnt/proc", 0755)
	os.MkdirAll("/mnt/sys", 0755)
	os.MkdirAll("/mnt/dev", 0755)
	os.MkdirAll("/mnt/run", 0755)

	if err := syscall.Mount("proc", "/mnt/proc", "proc", 0, ""); err != nil {
		return fmt.Errorf("failed to mount proc: %w", err)
	}
	if err := syscall.Mount("/sys", "/mnt/sys", "", syscall.MS_BIND|syscall.MS_REC, ""); err != nil {
		return fmt.Errorf("failed to mount sys: %w", err)
	}
	if err := syscall.Mount("/dev", "/mnt/dev", "", syscall.MS_BIND|syscall.MS_REC, ""); err != nil {
		return fmt.Errorf("failed to mount dev: %w", err)
	}
	if err := syscall.Mount("tmpfs", "/mnt/run", "tmpfs", 0, "mode=0755"); err != nil {
		return fmt.Errorf("failed to mount run: %w", err)
	}

	defer func() {
		syscall.Unmount("/mnt/run", 0)
		syscall.Unmount("/mnt/dev", 0)
		syscall.Unmount("/mnt/sys", 0)
		syscall.Unmount("/mnt/proc", 0)
	}()

	// Copy DNS configuration with dereference to handle symlinks (e.g., systemd-resolved)
	exec.Command("cp", "-L", "/etc/resolv.conf", "/mnt/etc/").Run()

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

	script += "chpasswd << 'EOF'\n"
	script += fmt.Sprintf("root:%s\n", cfg.RootPass)
	script += fmt.Sprintf("%s:%s\n", cfg.Username, cfg.Password)
	script += "EOF\n"

	script += `echo '%wheel ALL=(ALL:ALL) ALL' > /etc/sudoers.d/10-wheel
chmod 440 /etc/sudoers.d/10-wheel
`
	script += "mkinitrd\n"
	script += fmt.Sprintf("grub2-install --target=x86_64-efi --efi-directory=/boot/efi --bootloader-id=%s\n", distroName)
	if cfg.PartLayout == "dualboot" {
		script += "echo 'GRUB_DISABLE_OS_PROBER=false' >> /etc/default/grub\n"
	}
	script += "grub2-mkconfig -o /boot/grub2/grub.cfg\n"
	script += "systemctl enable NetworkManager\n"

	switch cfg.Desktop {
	case "KDE Plasma":
		script += "systemctl enable sddm\n"
	case "XFCE4":
		script += "systemctl enable lightdm\n"
	case "GNOME":
		script += "systemctl enable gdm\n"
	case "Hyprland":
		script += "systemctl enable sddm\n"
	}

	scriptPath := "/mnt/setup.sh"
	if err := os.WriteFile(scriptPath, []byte(script), 0700); err != nil {
		return err
	}
	defer os.Remove(scriptPath)

	fmt.Println(cyan + "[*] Processing sandbox environment options..." + reset)
	cmd := exec.Command("chroot", "/mnt", "/bin/bash", "/setup.sh")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

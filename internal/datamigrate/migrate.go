package datamigrate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"cmnet/internal/cache"
)

// Manifest records one timestamped backup so rollback can restore without guessing.
type Manifest struct {
	Version   int       `json:"version"`
	Stamp     string    `json:"stamp"`
	CreatedAt time.Time `json:"createdAt"`
	SourceDir string    `json:"sourceDir"`
	DestDir   string    `json:"destDir"`
	Kind      string    `json:"kind"` // "pre-migrate" | "pre-overwrite"
	Files     []string  `json:"files"`
	HasDB     bool      `json:"hasDb"`
	Note      string    `json:"note,omitempty"`
}

// Status is an honest report of Electron vs native locations and backup history.
type Status struct {
	ElectronDir   string      `json:"electronDir,omitempty"`
	NativeDir     string      `json:"nativeDir,omitempty"`
	DesktopDir    string      `json:"desktopDataDir,omitempty"`
	SameFolder    bool        `json:"sameFolder"`
	Source        DescribeDir `json:"source"`
	Destination   DescribeDir `json:"destination"`
	DBLocked      bool        `json:"dbLocked"`
	LockError     string      `json:"lockError,omitempty"`
	Backups       []string    `json:"backups,omitempty"`
	NeedsMigrate  bool        `json:"needsMigrate"`
	Advice        string      `json:"advice"`
}

// Options control migrate/run behaviour.
type Options struct {
	Source      string
	Dest        string
	Stamp       string // optional fixed stamp (tests); default UTC now
	SkipAssets    bool // when true, do not copy assets/
	UpdateDesktop bool // point desktop.json at Dest after success
	Now           func() time.Time
}

// ResolveDefaults fills Source/Dest from desktop.json + default native data dir
// when the caller left them empty. Prefers an existing desktop dataDir as Source.
func ResolveDefaults(opt *Options) error {
	if opt == nil {
		return errors.New("options is nil")
	}
	native, err := DefaultNativeDataDir()
	if err != nil {
		return err
	}
	desktop, ok, err := ReadDesktopDataDir()
	if err != nil {
		return err
	}
	if opt.Source == "" {
		if ok {
			opt.Source = desktop
		} else {
			opt.Source = native
		}
	}
	if opt.Dest == "" {
		opt.Dest = native
	}
	absSrc, err := filepath.Abs(opt.Source)
	if err != nil {
		return err
	}
	absDst, err := filepath.Abs(opt.Dest)
	if err != nil {
		return err
	}
	opt.Source, opt.Dest = absSrc, absDst
	if opt.Now == nil {
		opt.Now = func() time.Time { return time.Now().UTC() }
	}
	if opt.Stamp == "" {
		opt.Stamp = opt.Now().Format("20060102-150405")
	}
	return nil
}

// Inspect builds Status for the given options (after ResolveDefaults).
func Inspect(opt Options) (Status, error) {
	if err := ResolveDefaults(&opt); err != nil {
		return Status{}, err
	}
	src, err := Describe(opt.Source)
	if err != nil {
		return Status{}, err
	}
	dst, err := Describe(opt.Dest)
	if err != nil {
		return Status{}, err
	}
	st := Status{
		Source:      src,
		Destination: dst,
		SameFolder:  sameDir(opt.Source, opt.Dest),
		NativeDir:   opt.Dest,
		ElectronDir: opt.Source,
	}
	if desktop, ok, _ := ReadDesktopDataDir(); ok {
		st.DesktopDir = desktop
	}
	dbPath := filepath.Join(opt.Source, dbFileName)
	if src.HasDB {
		if err := cache.ProbeLock(dbPath); err != nil {
			if errors.Is(err, cache.ErrDatabaseBusy) {
				st.DBLocked = true
				st.LockError = err.Error()
			} else {
				st.LockError = err.Error()
			}
		}
	}
	st.Backups = ListBackups(opt.Dest)
	if len(st.Backups) == 0 && !sameDir(opt.Source, opt.Dest) {
		st.Backups = ListBackups(opt.Source)
	}
	st.NeedsMigrate = !st.SameFolder && src.HasDB && (!dst.HasDB || dst.DBBytes == 0)
	st.Advice = adviceFor(st)
	return st, nil
}

func adviceFor(st Status) string {
	if st.DBLocked {
		return "База занята другим процессом. Закройте Electron (и второй cmnet), затем повторите backup/migrate."
	}
	if !st.Source.HasDB {
		return "В исходной папке нет cmnet.db — мигрировать нечего. Укажите --from на папку Electron или выберите её в мастере первого запуска."
	}
	if st.SameFolder {
		return "Electron и native указывают на одну папку. Перед первым запуском native сделайте timestamped backup (advisor migrate backup). Не открывайте оба клиента одновременно."
	}
	if st.NeedsMigrate {
		return "Найдены данные Electron и пустая/отсутствующая native-папка. Выполните advisor migrate run (сначала backup)."
	}
	if st.Destination.HasDB {
		return "В назначении уже есть cmnet.db. migrate run сделает backup назначения и заменит артефакты из источника только после явного запуска."
	}
	return "Готово к backup/migrate."
}

// BackupDir is <dataDir>/migration-backup/<stamp>.
func BackupDir(dataDir, stamp string) string {
	return filepath.Join(dataDir, backupRootName, stamp)
}

// ListBackups returns stamp folders under dataDir/migration-backup, newest first.
func ListBackups(dataDir string) []string {
	root := filepath.Join(dataDir, backupRootName)
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if fileExists(filepath.Join(root, e.Name(), manifestName)) ||
			fileExists(filepath.Join(root, e.Name(), dbFileName)) {
			out = append(out, filepath.Join(root, e.Name()))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] > out[j] })
	return out
}

// CreateBackup writes a timestamped consistent backup of src into
// backupParent/migration-backup/<stamp>/. backupParent is usually Dest (or Source
// when same-folder). Always runs before migrate writes.
func CreateBackup(ctx context.Context, srcDir, backupParent, stamp, kind string) (Manifest, error) {
	src, err := Describe(srcDir)
	if err != nil {
		return Manifest{}, err
	}
	if !src.HasDB && len(src.Artifacts) == 0 {
		return Manifest{}, errors.New("нечего сохранять: нет cmnet.db и известных артефактов")
	}
	destRoot := BackupDir(backupParent, stamp)
	if _, err := os.Stat(destRoot); err == nil {
		return Manifest{}, fmt.Errorf("backup already exists: %s", destRoot)
	}
	if err := os.MkdirAll(destRoot, 0o755); err != nil {
		return Manifest{}, err
	}
	man := Manifest{
		Version:   1,
		Stamp:     stamp,
		CreatedAt: time.Now().UTC(),
		SourceDir: src.Dir,
		DestDir:   backupParent,
		Kind:      kind,
	}
	if src.HasDB {
		dbSrc := filepath.Join(src.Dir, dbFileName)
		dbDst := filepath.Join(destRoot, dbFileName)
		if err := cache.BackupPath(ctx, dbSrc, dbDst); err != nil {
			_ = os.RemoveAll(destRoot)
			return Manifest{}, err
		}
		man.HasDB = true
		man.Files = append(man.Files, dbFileName)
	}
	for _, name := range artifactFiles {
		from := filepath.Join(src.Dir, name)
		if !fileExists(from) {
			continue
		}
		to := filepath.Join(destRoot, name)
		if err := copyFile(from, to); err != nil {
			_ = os.RemoveAll(destRoot)
			return Manifest{}, fmt.Errorf("backup %s: %w", name, err)
		}
		man.Files = append(man.Files, name)
	}
	assetsSrc := filepath.Join(src.Dir, assetsDirName)
	if dirExists(assetsSrc) {
		if err := copyDir(assetsSrc, filepath.Join(destRoot, assetsDirName)); err != nil {
			_ = os.RemoveAll(destRoot)
			return Manifest{}, fmt.Errorf("backup assets: %w", err)
		}
		man.Files = append(man.Files, assetsDirName+"/")
	}
	man.Note = "Restore with: advisor migrate rollback --backup " + destRoot
	if err := writeManifest(destRoot, man); err != nil {
		_ = os.RemoveAll(destRoot)
		return Manifest{}, err
	}
	return man, nil
}

// Result summarises a migrate run.
type Result struct {
	Backup   Manifest `json:"backup"`
	Copied   []string `json:"copied"`
	Source   string   `json:"source"`
	Dest     string   `json:"dest"`
	SameDir  bool     `json:"sameDir"`
	Desktop  string   `json:"desktopDataDir,omitempty"`
	Message  string   `json:"message"`
}

// Run performs: lock check → backup destination (or source if same) → copy
// artifacts from Source to Dest (skipped when same folder after backup-only).
func Run(ctx context.Context, opt Options) (Result, error) {
	if err := ResolveDefaults(&opt); err != nil {
		return Result{}, err
	}
	src, err := Describe(opt.Source)
	if err != nil {
		return Result{}, err
	}
	if !src.HasDB {
		return Result{}, errors.New("источник без cmnet.db — отказ (не изобретаем данные)")
	}
	dbPath := filepath.Join(opt.Source, dbFileName)
	if err := cache.ProbeLock(dbPath); err != nil {
		return Result{}, err
	}
	same := sameDir(opt.Source, opt.Dest)
	backupParent := opt.Dest
	kind := "pre-migrate"
	if same {
		backupParent = opt.Source
		kind = "pre-overwrite"
	}
	if destDB := filepath.Join(opt.Dest, dbFileName); fileExists(destDB) {
		if err := cache.ProbeLock(destDB); err != nil {
			return Result{}, err
		}
	}

	man, err := CreateBackup(ctx, backupContentDir(opt, same), backupParent, opt.Stamp, kind)
	if err != nil {
		return Result{}, err
	}

	res := Result{
		Backup:  man,
		Source:  opt.Source,
		Dest:    opt.Dest,
		SameDir: same,
	}
	if same {
		res.Message = "Одна папка: создан timestamped backup. Данные не копировались. Не запускайте Electron параллельно с native."
		res.Copied = nil
		if opt.UpdateDesktop {
			if err := WriteDesktopDataDir(opt.Dest); err != nil {
				return res, err
			}
			res.Desktop = opt.Dest
		}
		return res, nil
	}

	if err := os.MkdirAll(opt.Dest, 0o755); err != nil {
		return res, err
	}
	// Replace destination DB via consistent backup from source (not raw copy).
	destDB := filepath.Join(opt.Dest, dbFileName)
	_ = os.Remove(destDB)
	_ = os.Remove(destDB + "-wal")
	_ = os.Remove(destDB + "-shm")
	if err := cache.BackupPath(ctx, dbPath, destDB); err != nil {
		return res, fmt.Errorf("copy database: %w", err)
	}
	res.Copied = append(res.Copied, dbFileName)

	for _, name := range artifactFiles {
		from := filepath.Join(opt.Source, name)
		if !fileExists(from) {
			continue
		}
		to := filepath.Join(opt.Dest, name)
		if err := copyFile(from, to); err != nil {
			return res, fmt.Errorf("copy %s: %w", name, err)
		}
		res.Copied = append(res.Copied, name)
	}
	assetsSrc := filepath.Join(opt.Source, assetsDirName)
	if !opt.SkipAssets && dirExists(assetsSrc) {
		if err := copyDir(assetsSrc, filepath.Join(opt.Dest, assetsDirName)); err != nil {
			return res, fmt.Errorf("copy assets: %w", err)
		}
		res.Copied = append(res.Copied, assetsDirName+"/")
	}
	if opt.UpdateDesktop {
		if err := WriteDesktopDataDir(opt.Dest); err != nil {
			return res, err
		}
		res.Desktop = opt.Dest
	}
	res.Message = fmt.Sprintf("Мигрировано %d артефакт(ов) → %s. Backup: %s. Rollback: advisor migrate rollback --backup %s",
		len(res.Copied), opt.Dest, BackupDir(backupParent, opt.Stamp), BackupDir(backupParent, opt.Stamp))
	return res, nil
}

func backupContentDir(opt Options, same bool) string {
	if same {
		return opt.Source
	}
	// Prefer backing up destination if it already has data; else snapshot source
	// so rollback can undo a partial copy into an empty dest.
	if dst, err := Describe(opt.Dest); err == nil && (dst.HasDB || len(dst.Artifacts) > 0) {
		return opt.Dest
	}
	return opt.Source
}

// Rollback restores files listed in the backup manifest into destDir.
// Refuses when dest cmnet.db is locked. Does not invent missing manifest entries.
func Rollback(ctx context.Context, backupDir, destDir string) (Manifest, error) {
	man, err := ReadManifest(backupDir)
	if err != nil {
		return Manifest{}, err
	}
	if destDir == "" {
		destDir = man.DestDir
	}
	if destDir == "" {
		return Manifest{}, errors.New("некуда откатывать: укажите --to")
	}
	absDest, err := filepath.Abs(destDir)
	if err != nil {
		return Manifest{}, err
	}
	if err := os.MkdirAll(absDest, 0o755); err != nil {
		return Manifest{}, err
	}
	destDB := filepath.Join(absDest, dbFileName)
	if fileExists(destDB) {
		if err := cache.ProbeLock(destDB); err != nil {
			return Manifest{}, err
		}
	}
	for _, name := range man.Files {
		name = strings.TrimSuffix(name, "/")
		from := filepath.Join(backupDir, name)
		to := filepath.Join(absDest, name)
		if name == assetsDirName {
			if !dirExists(from) {
				continue
			}
			_ = os.RemoveAll(to)
			if err := copyDir(from, to); err != nil {
				return man, fmt.Errorf("rollback assets: %w", err)
			}
			continue
		}
		if name == dbFileName {
			_ = os.Remove(to)
			_ = os.Remove(to + "-wal")
			_ = os.Remove(to + "-shm")
			// Backup DB is already a consistent VACUUM snapshot — copy bytes.
			if err := copyFile(from, to); err != nil {
				return man, fmt.Errorf("rollback db: %w", err)
			}
			continue
		}
		if !fileExists(from) {
			return man, fmt.Errorf("в backup нет файла %s", name)
		}
		if err := copyFile(from, to); err != nil {
			return man, fmt.Errorf("rollback %s: %w", name, err)
		}
	}
	_ = ctx
	man.Note = "Restored into " + absDest
	return man, nil
}

func writeManifest(dir string, man Manifest) error {
	body, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, manifestName), body, 0o644)
}

// ReadManifest loads MANIFEST.json from a backup folder.
func ReadManifest(backupDir string) (Manifest, error) {
	raw, err := os.ReadFile(filepath.Join(backupDir, manifestName))
	if err != nil {
		return Manifest{}, fmt.Errorf("manifest: %w", err)
	}
	var man Manifest
	if err := json.Unmarshal(raw, &man); err != nil {
		return Manifest{}, err
	}
	return man, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	tmp := dst + ".part"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	_ = os.Remove(dst)
	return os.Rename(tmp, dst)
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

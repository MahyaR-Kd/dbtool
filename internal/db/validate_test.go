package db

import (
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// cartsSchemaSQL reproduces the exact CREATE TABLE shape from the real
// mydumper 0.10.0 / MySQL 8.0.36 bug report this check was built to catch
// (column names, types, and defaults all match) — row values below are
// synthetic, not the original report's real data.
const cartsSchemaSQL = "CREATE TABLE `carts` (\n" +
	"  `id` int NOT NULL AUTO_INCREMENT,\n" +
	"  `uuid` varchar(36) NOT NULL,\n" +
	"  `store_id` int DEFAULT NULL,\n" +
	"  `contact_id` int DEFAULT NULL,\n" +
	"  `grand_total` decimal(10,2) unsigned NOT NULL DEFAULT '0.00',\n" +
	"  `job_status` enum('COMPLETED','WAITING','ACTIVE','DELAYED','FAILED','PAUSED') NOT NULL DEFAULT 'COMPLETED',\n" +
	"  `status` enum('CONVERTED','CONVERTING','CANCELED','DRAFT') NOT NULL DEFAULT 'DRAFT',\n" +
	"  `current_lock_access_id` int DEFAULT NULL,\n" +
	"  `is_active` tinyint NOT NULL DEFAULT '1',\n" +
	"  `created_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),\n" +
	"  `updated_at` datetime(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6),\n" +
	"  `deleted_at` datetime(6) DEFAULT NULL,\n" +
	"  `metadata` json DEFAULT NULL,\n" +
	"  `created_by_admin` varchar(255) DEFAULT NULL,\n" +
	"  `updated_by_admin` varchar(255) DEFAULT NULL,\n" +
	"  `deleted_by_admin` varchar(255) DEFAULT NULL,\n" +
	"  `created_by_user` varchar(255) DEFAULT NULL,\n" +
	"  `updated_by_user` varchar(255) DEFAULT NULL,\n" +
	"  `deleted_by_user` varchar(255) DEFAULT NULL,\n" +
	"  `source` enum('STORE_FRONT','BACK_OFFICE','ORDER') NOT NULL DEFAULT 'BACK_OFFICE',\n" +
	"  `conversion_lock_owner` varchar(255) DEFAULT NULL,\n" +
	"  `conversion_locked_at` datetime DEFAULT NULL,\n" +
	"  PRIMARY KEY (`id`),\n" +
	"  UNIQUE KEY `REL_dce098b9d22a2a73f8e867a085` (`current_lock_access_id`),\n" +
	"  CONSTRAINT `FK_dce098b9d22a2a73f8e867a085c` FOREIGN KEY (`current_lock_access_id`) REFERENCES `cart_lock_accesses` (`id`)\n" +
	") ENGINE=InnoDB AUTO_INCREMENT=2 DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci"

// cartsBuggyInsertSQL reproduces the exact (buggy) INSERT column list from
// the same report, with synthetic row values — note created_at and
// updated_at are missing from the column list, which is the bug itself.
const cartsBuggyInsertSQL = "/*!40101 SET NAMES binary*/;\n" +
	"/*!40014 SET FOREIGN_KEY_CHECKS=0*/;\n" +
	"/*!40103 SET TIME_ZONE='+00:00' */;\n" +
	"INSERT INTO `carts` (`contact_id`,`conversion_lock_owner`,`conversion_locked_at`,`created_by_admin`,`created_by_user`,`current_lock_access_id`,`deleted_at`,`deleted_by_admin`,`deleted_by_user`,`grand_total`,`id`,`is_active`,`job_status`,`metadata`,`source`,`status`,`store_id`,`updated_by_admin`,`updated_by_user`,`uuid`) VALUES\n" +
	"(NULL,NULL,NULL,\"00000000-0000-4000-8000-000000000001\",NULL,NULL,\"2024-01-01\",NULL,NULL,10.00,1,1,\"COMPLETED\",'{}',\"STORE_FRONT\",\"DRAFT\",1,NULL,NULL,\"00000000-0000-4000-8000-000000000002\");\n"

func writeDumpFile(t *testing.T, dir, name, content string, gz bool) {
	t.Helper()
	path := filepath.Join(dir, name)
	if !gz {
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		return
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	defer f.Close()
	gw := gzip.NewWriter(f)
	if _, err := gw.Write([]byte(content)); err != nil {
		t.Fatalf("gzip write %s: %v", name, err)
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gzip close %s: %v", name, err)
	}
}

// writeDumpFileZst mirrors writeDumpFile for zstd-compressed files.
func writeDumpFileZst(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	defer f.Close()
	zw, err := zstd.NewWriter(f)
	if err != nil {
		t.Fatalf("zstd.NewWriter %s: %v", name, err)
	}
	if _, err := zw.Write([]byte(content)); err != nil {
		t.Fatalf("zstd write %s: %v", name, err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zstd close %s: %v", name, err)
	}
}

// TestValidateDump_DetectsRealWorldCartsBug_ZstCompressed guards against a
// real bug: mydumper's --compress flag can produce .zst output instead of
// .gz depending on version (observed on a real mydumper 1.0.5 build) —
// before this package understood .zst, ValidateDump silently skipped these
// files entirely (openCompressed only recognized ".gz"), so the exact
// column-dropped safety net this test exercises would never have fired on
// a real zstd-compressed dump.
func TestValidateDump_DetectsRealWorldCartsBug_ZstCompressed(t *testing.T) {
	dir := t.TempDir()
	writeDumpFileZst(t, dir, "shopping_cart.carts-schema.sql.zst", cartsSchemaSQL)
	writeDumpFileZst(t, dir, "shopping_cart.carts.sql.zst", cartsBuggyInsertSQL)

	issues := ValidateDump(dir)
	if len(issues) != 1 {
		t.Fatalf("got %d issues, want 1: %+v", len(issues), issues)
	}
	if issues[0].Table != "carts" {
		t.Errorf("table = %q, want %q", issues[0].Table, "carts")
	}
	want := map[string]bool{"created_at": true, "updated_at": true}
	if len(issues[0].Missing) != len(want) {
		t.Fatalf("missing = %v, want exactly %v", issues[0].Missing, want)
	}
	for _, m := range issues[0].Missing {
		if !want[m] {
			t.Errorf("unexpected missing column %q", m)
		}
	}
}

func TestValidateDump_DetectsRealWorldCartsBug(t *testing.T) {
	dir := t.TempDir()
	writeDumpFile(t, dir, "shopping_cart.carts-schema.sql.gz", cartsSchemaSQL, true)
	writeDumpFile(t, dir, "shopping_cart.carts.sql.gz", cartsBuggyInsertSQL, true)

	issues := ValidateDump(dir)
	if len(issues) != 1 {
		t.Fatalf("got %d issues, want 1: %+v", len(issues), issues)
	}
	if issues[0].Table != "carts" {
		t.Errorf("table = %q, want %q", issues[0].Table, "carts")
	}
	want := map[string]bool{"created_at": true, "updated_at": true}
	if len(issues[0].Missing) != len(want) {
		t.Fatalf("missing = %v, want exactly %v", issues[0].Missing, want)
	}
	for _, m := range issues[0].Missing {
		if !want[m] {
			t.Errorf("unexpected missing column %q", m)
		}
	}
}

func TestValidateDump_NoIssueWhenAllColumnsPresent(t *testing.T) {
	dir := t.TempDir()
	writeDumpFile(t, dir, "shopping_cart.carts-schema.sql.gz", cartsSchemaSQL, true)

	fixedInsert := "INSERT INTO `carts` (`id`,`uuid`,`store_id`,`contact_id`,`grand_total`,`job_status`,`status`,`current_lock_access_id`,`is_active`,`created_at`,`updated_at`,`deleted_at`,`metadata`,`created_by_admin`,`updated_by_admin`,`deleted_by_admin`,`created_by_user`,`updated_by_user`,`deleted_by_user`,`source`,`conversion_lock_owner`,`conversion_locked_at`) VALUES\n" +
		"(1,\"00000000-0000-4000-8000-000000000002\",1,NULL,10.00,\"COMPLETED\",\"DRAFT\",NULL,1,\"2024-01-01 00:00:00.000000\",\"2024-01-01 00:05:00.000000\",NULL,'{}',NULL,NULL,NULL,NULL,NULL,NULL,\"STORE_FRONT\",NULL,NULL);\n"
	writeDumpFile(t, dir, "shopping_cart.carts.sql.gz", fixedInsert, true)

	issues := ValidateDump(dir)
	if len(issues) != 0 {
		t.Fatalf("got %d issues, want 0: %+v", len(issues), issues)
	}
}

func TestValidateDump_TrueGeneratedColumnIsNotFlagged(t *testing.T) {
	dir := t.TempDir()
	schema := "CREATE TABLE `orders` (\n" +
		"  `id` int NOT NULL AUTO_INCREMENT,\n" +
		"  `price` decimal(10,2) NOT NULL,\n" +
		"  `qty` int NOT NULL,\n" +
		"  `total` decimal(10,2) GENERATED ALWAYS AS (`price` * `qty`) STORED,\n" +
		"  PRIMARY KEY (`id`)\n" +
		") ENGINE=InnoDB DEFAULT CHARSET=utf8mb4"
	writeDumpFile(t, dir, "mydb.orders-schema.sql.gz", schema, true)

	// `total` is a real generated column — mydumper correctly omits it.
	insert := "INSERT INTO `orders` (`id`,`price`,`qty`) VALUES\n(1,9.99,2);\n"
	writeDumpFile(t, dir, "mydb.orders.sql.gz", insert, true)

	issues := ValidateDump(dir)
	if len(issues) != 0 {
		t.Fatalf("got %d issues for a legitimate generated column, want 0: %+v", len(issues), issues)
	}
}

func TestValidateDump_PositionalInsertSkipped(t *testing.T) {
	dir := t.TempDir()
	schema := "CREATE TABLE `simple` (\n" +
		"  `id` int NOT NULL,\n" +
		"  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,\n" +
		"  PRIMARY KEY (`id`)\n" +
		") ENGINE=InnoDB"
	writeDumpFile(t, dir, "mydb.simple-schema.sql.gz", schema, true)

	// Positional INSERT (no column list) — always includes every column.
	insert := "INSERT INTO `simple` VALUES\n(1,\"2020-01-01 00:00:00\");\n"
	writeDumpFile(t, dir, "mydb.simple.sql.gz", insert, true)

	issues := ValidateDump(dir)
	if len(issues) != 0 {
		t.Fatalf("got %d issues for a positional INSERT, want 0: %+v", len(issues), issues)
	}
}

func TestValidateDump_EmptyTableSkipped(t *testing.T) {
	dir := t.TempDir()
	schema := "CREATE TABLE `empty_table` (\n" +
		"  `id` int NOT NULL,\n" +
		"  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,\n" +
		"  PRIMARY KEY (`id`)\n" +
		") ENGINE=InnoDB"
	writeDumpFile(t, dir, "mydb.empty_table-schema.sql.gz", schema, true)

	// mydumper writes just the pragma preamble for a table with zero rows.
	noRows := "/*!40101 SET NAMES binary*/;\n/*!40014 SET FOREIGN_KEY_CHECKS=0*/;\n"
	writeDumpFile(t, dir, "mydb.empty_table.sql.gz", noRows, true)

	issues := ValidateDump(dir)
	if len(issues) != 0 {
		t.Fatalf("got %d issues for an empty table, want 0: %+v", len(issues), issues)
	}
}

func TestValidateDump_UncompressedFilesWork(t *testing.T) {
	dir := t.TempDir()
	schema := "CREATE TABLE `plain` (\n" +
		"  `id` int NOT NULL,\n" +
		"  `created_at` datetime NOT NULL DEFAULT CURRENT_TIMESTAMP,\n" +
		"  PRIMARY KEY (`id`)\n" +
		") ENGINE=InnoDB"
	writeDumpFile(t, dir, "mydb.plain-schema.sql", schema, false)

	insert := "INSERT INTO `plain` (`id`) VALUES\n(1);\n"
	writeDumpFile(t, dir, "mydb.plain.sql", insert, false)

	issues := ValidateDump(dir)
	if len(issues) != 1 {
		t.Fatalf("got %d issues, want 1 (missing created_at): %+v", len(issues), issues)
	}
	if issues[0].Table != "plain" || len(issues[0].Missing) != 1 || issues[0].Missing[0] != "created_at" {
		t.Errorf("unexpected issue: %+v", issues[0])
	}
}

func TestValidateDump_NoSchemaFilesReturnsNil(t *testing.T) {
	dir := t.TempDir()
	issues := ValidateDump(dir)
	if issues != nil {
		t.Errorf("got %v, want nil for an empty dump dir", issues)
	}
}

func TestFindFirstDataFile_PicksLowestChunkDeterministically(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{
		"mydb.big-schema.sql.gz",
		"mydb.big.00001.sql.gz",
		"mydb.big.00000.sql.gz",
	} {
		writeDumpFile(t, dir, name, "x", true)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	got := findFirstDataFile(entries, "mydb.big")
	if got != "mydb.big.00000.sql.gz" {
		t.Errorf("got %q, want %q", got, "mydb.big.00000.sql.gz")
	}
}

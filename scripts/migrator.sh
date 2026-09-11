#!/usr/bin/env sh
# set -eu # Exit on error, treat unset variables as an error.

#------------------------------------------------------------------------------
# Database Migration & Backup Script (by Aris Ripandi <aris@duck.com>)
#------------------------------------------------------------------------------
# A wrapper script for Goose migrations with PostgreSQL backup/restore utilities.
#
# Features:
#   - Database migrations using Goose (up, down, reset, status, create, etc.)
#   - Dump/Restore: Binary format backup (custom format, faster, smaller)
#   - Export/Import: SQL format backup (plain text, portable, editable)
#
# Requirements:
#   - goose: Database migration tool
#   - pg_dump, pg_restore, psql: PostgreSQL utilities
#
# Configuration:
#   - GOOSE_DBSTRING: PostgreSQL connection string (from env or default)
#   - GOOSE_MIGRATION_DIR: Directory for migration files (default: ./database/migrations)
#   - GOOSE_TABLE: Migration tracking table (default: app_migration)
#
# Backup location: ./storage/backup/
#
# Usage: ./migrator.sh [command] [args...]
#        Run './migrator.sh help' for available commands
#------------------------------------------------------------------------------

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Path of current script and project root
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

# # Load .env.local if exists
# if [ -f "$ROOT_DIR/.env.local" ]; then
#   # shellcheck disable=SC1091
#   . "$ROOT_DIR/.env.local"
# fi

# Goose migration environment
export GOOSE_DRIVER="postgres"
export GOOSE_DBSTRING="${GOOSE_DBSTRING:-${DATABASE_URL:-postgresql://postgres:postgres@localhost:5432/postgres?sslmode=disable}}"
export GOOSE_MIGRATION_DIR="$ROOT_DIR/database/migrations"
export GOOSE_TABLE="app_migration"

GOOSE_BIN="${GOOSE_BIN:-goose}"
PG_DUMP_BIN=$(which pg_dump)
PG_RESTORE_BIN=$(which pg_restore)
PSQL_BIN=$(which psql)

# Backup directory
BACKUP_DIR="$ROOT_DIR/storage/backup"
mkdir -p "$BACKUP_DIR"

# Parse connection string for pg_dump/pg_restore
parse_pg_url() {
    # Example: postgresql://user:pass@host:port/dbname?sslmode=disable
    url="$1"
    proto_removed="${url#*://}"
    userpass_hostport_db="${proto_removed%%\?*}" # Remove query params

    # Extract user:pass@host:port/dbname
    if echo "$userpass_hostport_db" | grep -q "@"; then
        userpass="${userpass_hostport_db%%@*}"
        hostport_db="${userpass_hostport_db#*@}"
    else
        userpass=""
        hostport_db="$userpass_hostport_db"
    fi

    if [ -n "$userpass" ]; then
        USER="${userpass%%:*}"
        PASS="${userpass#*:}"
        [ "$USER" = "$PASS" ] && PASS=""
    else
        USER="postgres"
        PASS=""
    fi

    HOSTPORT="${hostport_db%%/*}"
    DBNAME="${hostport_db#*/}"

    HOST="${HOSTPORT%%:*}"
    PORT="${HOSTPORT#*:}"
    [ "$PORT" = "$HOSTPORT" ] && PORT="5432"
}

parse_pg_url "$GOOSE_DBSTRING"

# Check if goose binary exists
if ! command -v "$GOOSE_BIN" >/dev/null 2>&1; then
    printf "${RED}Error:${NC} goose binary not found. Please install goose (https://github.com/pressly/goose) and ensure it is in your PATH.\n"
    exit 127
fi

# Check and prepare migration directory
if [ ! -d "$GOOSE_MIGRATION_DIR" ]; then
    printf "${YELLOW}Migration directory '$GOOSE_MIGRATION_DIR' does not exist. Creating...${NC}\n"
    mkdir -p "$GOOSE_MIGRATION_DIR"
fi

usage() {
    printf "${CYAN}Usage:${NC} $0 [command] [args...]\n"
    printf "${CYAN}Commands:${NC}\n"
    printf "  ${GREEN}up [N]${NC}               Apply all or N up migrations\n"
    printf "  ${GREEN}down [N]${NC}             Roll back N migrations\n"
    printf "  ${GREEN}reset${NC}                Roll back all migrations\n"
    printf "  ${GREEN}status${NC}               Print current migration status\n"
    printf "  ${GREEN}create NAME${NC}          Create a new migration\n"
    printf "  ${GREEN}fix${NC}                  Reorder migrations\n"
    printf "  ${GREEN}validate${NC}             Check the migration files\n"
    printf "  ${GREEN}version${NC}              Print current migration version\n"
    printf "  ${GREEN}dump [all|data]${NC}      Dump schema & data or data only (custom format)\n"
    printf "  ${GREEN}restore [all|data|schema] <dump_file>${NC}  Restore from dump file (custom format)\n"
    printf "  ${GREEN}export [all|data]${NC}    Export schema & data or data only (SQL format)\n"
    printf "  ${GREEN}import <sql_file>${NC}    Import from SQL file\n"
    printf "  ${GREEN}help${NC}                 Show this help\n"
}

dump_usage() {
    printf "${CYAN}Usage:${NC} $0 dump [all|data]\n"
    printf "  ${GREEN}all${NC}   Dump schema & data (custom format)\n"
    printf "  ${GREEN}data${NC}  Dump data only (custom format)\n"
}

restore_usage() {
    printf "${CYAN}Usage:${NC} $0 restore [all|data|schema] <dump_file>\n"
    printf "  ${GREEN}all${NC}     Restore schema & data\n"
    printf "  ${GREEN}data${NC}    Restore data only\n"
    printf "  ${GREEN}schema${NC}  Restore schema only\n"
}

export_usage() {
    printf "${CYAN}Usage:${NC} $0 export [all|data]\n"
    printf "  ${GREEN}all${NC}   Export schema & data (SQL format)\n"
    printf "  ${GREEN}data${NC}  Export data only (SQL format)\n"
}

import_usage() {
    printf "${CYAN}Usage:${NC} $0 import <sql_file>\n"
    printf "  Import SQL file into database\n"
}

if [ $# -eq 0 ] || [ "$1" = "help" ]; then
    usage
    exit 0
fi

CMD="$1"
shift

case "$CMD" in
up)
    printf "${YELLOW}Running goose up...${NC}\n"
    exec "$GOOSE_BIN" -table "$GOOSE_TABLE" -dir "$GOOSE_MIGRATION_DIR" up "$@"
    ;;
down)
    printf "${YELLOW}Running goose down...${NC}\n"
    exec "$GOOSE_BIN" -table "$GOOSE_TABLE" -dir "$GOOSE_MIGRATION_DIR" down "$@"
    ;;
reset)
    printf "${YELLOW}Checking migration status before reset...${NC}\n"
    "$GOOSE_BIN" -table "$GOOSE_TABLE" -dir "$GOOSE_MIGRATION_DIR" status
    printf "${YELLOW}Running goose reset...${NC}\n"
    exec "$GOOSE_BIN" -table "$GOOSE_TABLE" -dir "$GOOSE_MIGRATION_DIR" reset
    ;;
status)
    printf "${YELLOW}Running goose status...${NC}\n"
    exec "$GOOSE_BIN" -table "$GOOSE_TABLE" -dir "$GOOSE_MIGRATION_DIR" status
    ;;
create)
    if [ $# -eq 0 ]; then
        printf "${RED}Missing migration name.${NC}\n"
        exit 1
    fi
    MIGRATION_NAME="$1"
    EXISTING=$(find "$GOOSE_MIGRATION_DIR" -type f -name "*.sql" | grep -iE "[0-9]+_${MIGRATION_NAME}\.sql" || true)
    if [ -n "$EXISTING" ]; then
        printf "${RED}Error:${NC} Migration with name '${MIGRATION_NAME}' already exists:\n"
        printf "%s\n" "$EXISTING"
        exit 2
    fi
    printf "${YELLOW}Creating new migration: $MIGRATION_NAME ...${NC}\n"
    exec "$GOOSE_BIN" -dir "$GOOSE_MIGRATION_DIR" -s create "$MIGRATION_NAME" sql
    ;;
fix)
    printf "${YELLOW}Running goose fix...${NC}\n"
    exec "$GOOSE_BIN" -dir "$GOOSE_MIGRATION_DIR" fix
    ;;
validate)
    printf "${YELLOW}Running goose validate...${NC}\n"
    exec "$GOOSE_BIN" -dir "$GOOSE_MIGRATION_DIR" validate
    ;;
version)
    printf "${YELLOW}Running goose version...${NC}\n"
    exec "$GOOSE_BIN" -table "$GOOSE_TABLE" -dir "$GOOSE_MIGRATION_DIR" version
    ;;
dump)
    if [ $# -lt 1 ]; then
        dump_usage
        exit 1
    fi
    TYPE="$1"

    # Generate timestamp
    TIMESTAMP=$(date +"%Y%m%d_%H%M%S")

    # Build connection string options
    PG_OPTS="-U $USER -h $HOST -p $PORT"
    [ -n "$PASS" ] && export PGPASSWORD="$PASS"

    # Exclude PostgreSQL system schemas
    EXCLUDE_SCHEMAS="-N information_schema -N pg_catalog -N pg_toast"

    case "$TYPE" in
    all)
        DUMPFILE="$BACKUP_DIR/${DBNAME}_full_${TIMESTAMP}.dump"
        RELATIVE_PATH="${DUMPFILE#$ROOT_DIR/}"
        printf "${YELLOW}Dumping all schemas & data to: ${RELATIVE_PATH}${NC}\n"
        $PG_DUMP_BIN $PG_OPTS -d "$DBNAME" -F c -f "$DUMPFILE" $EXCLUDE_SCHEMAS --no-owner --no-acl
        printf "${GREEN}Dump completed: ${RELATIVE_PATH}${NC}\n"
        ;;
    data)
        DUMPFILE="$BACKUP_DIR/${DBNAME}_data_${TIMESTAMP}.dump"
        RELATIVE_PATH="${DUMPFILE#$ROOT_DIR/}"
        printf "${YELLOW}Dumping all schemas data only to: ${RELATIVE_PATH}${NC}\n"
        $PG_DUMP_BIN $PG_OPTS -d "$DBNAME" --data-only -F c -f "$DUMPFILE" $EXCLUDE_SCHEMAS --no-owner --no-acl
        printf "${GREEN}Dump completed: ${RELATIVE_PATH}${NC}\n"
        ;;
    *)
        dump_usage
        exit 1
        ;;
    esac
    unset PGPASSWORD
    ;;
restore)
    if [ $# -lt 2 ]; then
        restore_usage
        exit 1
    fi
    TYPE="$1"
    DUMPFILE="$2"

    # Handle relative path
    case "$DUMPFILE" in
    /*) ABS_DUMPFILE="$DUMPFILE" ;;
    *) ABS_DUMPFILE="$ROOT_DIR/$DUMPFILE" ;;
    esac

    if [ ! -f "$ABS_DUMPFILE" ]; then
        printf "${RED}Error:${NC} Dump file not found: $DUMPFILE\n"
        exit 1
    fi

    RELATIVE_PATH="${ABS_DUMPFILE#$ROOT_DIR/}"

    # Build connection string options
    PG_OPTS="-U $USER -h $HOST -p $PORT"
    [ -n "$PASS" ] && export PGPASSWORD="$PASS"

    # Exclude PostgreSQL system schemas
    EXCLUDE_SCHEMAS="-N information_schema -N pg_catalog -N pg_toast"

    case "$TYPE" in
    all)
        printf "${YELLOW}Restoring all schemas & data from: ${RELATIVE_PATH}${NC}\n"
        $PG_RESTORE_BIN $PG_OPTS -d "$DBNAME" --clean --if-exists --no-owner --no-acl $EXCLUDE_SCHEMAS "$ABS_DUMPFILE"
        printf "${GREEN}Restore completed${NC}\n"
        ;;
    data)
        printf "${YELLOW}Restoring all schemas data only from: ${RELATIVE_PATH}${NC}\n"
        $PG_RESTORE_BIN $PG_OPTS -d "$DBNAME" --data-only --disable-triggers --no-owner --no-acl $EXCLUDE_SCHEMAS "$ABS_DUMPFILE"
        printf "${GREEN}Restore completed${NC}\n"
        ;;
    schema)
        printf "${YELLOW}Restoring schema only from: ${RELATIVE_PATH}${NC}\n"
        $PG_RESTORE_BIN $PG_OPTS -d "$DBNAME" --schema-only --clean --if-exists --no-owner --no-acl $EXCLUDE_SCHEMAS "$ABS_DUMPFILE"
        printf "${GREEN}Restore completed${NC}\n"
        ;;
    *)
        restore_usage
        exit 1
        ;;
    esac
    unset PGPASSWORD
    ;;
export)
    if [ $# -lt 1 ]; then
        export_usage
        exit 1
    fi
    TYPE="$1"

    # Generate timestamp
    TIMESTAMP=$(date +"%Y%m%d_%H%M%S")

    # Build connection string options
    PG_OPTS="-U $USER -h $HOST -p $PORT"
    [ -n "$PASS" ] && export PGPASSWORD="$PASS"

    # Exclude PostgreSQL system schemas
    EXCLUDE_SCHEMAS="-N information_schema -N pg_catalog -N pg_toast"

    case "$TYPE" in
    all)
        SQLFILE="$BACKUP_DIR/${DBNAME}_full_${TIMESTAMP}.sql"
        RELATIVE_PATH="${SQLFILE#$ROOT_DIR/}"
        printf "${YELLOW}Exporting all schemas & data to: ${RELATIVE_PATH}${NC}\n"
        $PG_DUMP_BIN $PG_OPTS -d "$DBNAME" -f "$SQLFILE" $EXCLUDE_SCHEMAS \
            --no-owner --no-acl --clean --if-exists
        printf "${GREEN}Export completed: ${RELATIVE_PATH}${NC}\n"
        ;;
    data)
        SQLFILE="$BACKUP_DIR/${DBNAME}_data_${TIMESTAMP}.sql"
        RELATIVE_PATH="${SQLFILE#$ROOT_DIR/}"
        printf "${YELLOW}Exporting all schemas data only to: ${RELATIVE_PATH}${NC}\n"
        $PG_DUMP_BIN $PG_OPTS -d "$DBNAME" --data-only -f "$SQLFILE" $EXCLUDE_SCHEMAS \
            --no-owner --no-acl --column-inserts
        printf "${GREEN}Export completed: ${RELATIVE_PATH}${NC}\n"
        ;;
    *)
        export_usage
        exit 1
        ;;
    esac
    unset PGPASSWORD
    ;;
import)
    if [ $# -lt 1 ]; then
        import_usage
        exit 1
    fi
    SQLFILE="$1"

    # Handle relative path
    case "$SQLFILE" in
    /*) ABS_SQLFILE="$SQLFILE" ;;
    *) ABS_SQLFILE="$ROOT_DIR/$SQLFILE" ;;
    esac

    if [ ! -f "$ABS_SQLFILE" ]; then
        printf "${RED}Error:${NC} SQL file not found: $SQLFILE\n"
        exit 1
    fi

    RELATIVE_PATH="${ABS_SQLFILE#$ROOT_DIR/}"

    # Build connection string options
    PG_OPTS="-U $USER -h $HOST -p $PORT"
    [ -n "$PASS" ] && export PGPASSWORD="$PASS"

    printf "${YELLOW}Importing SQL from: ${RELATIVE_PATH}${NC}\n"
    $PSQL_BIN $PG_OPTS -d "$DBNAME" -f "$ABS_SQLFILE" \
        --variable=ON_ERROR_STOP=off \
        --quiet
    printf "${GREEN}Import completed${NC}\n"

    unset PGPASSWORD
    ;;
*)
    usage
    exit 1
    ;;
esac

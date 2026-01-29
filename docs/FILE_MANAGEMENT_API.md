# Pelican Wings - File Management API

This document covers all file management endpoints available in the Pelican Wings API.

## Table of Contents

1. [Authentication](#authentication)
2. [File Operations](#file-operations)
   - [Read File Contents](#get-apiserversserverfilescontents)
   - [List Directory](#get-apiserversserverfileslist-directory)
   - [Write File](#post-apiserversserverfileswrite)
   - [Rename/Move Files](#put-apiserversserverfilesrename)
   - [Copy File](#post-apiserversserverfilescopy)
   - [Delete Files](#post-apiserversserverfilesdelete)
   - [Create Directory](#post-apiserversserverfilescreate-directory)
3. [Archive Operations](#archive-operations)
   - [Compress Files](#post-apiserversserverfilescompress)
   - [Decompress Archive](#post-apiserversserverfilesdecompress)
4. [Permissions](#permissions)
   - [Change Permissions](#post-apiserversserverfileschmod)
5. [Search](#search)
   - [Search Files](#get-apiserversserverfilessearch)
6. [Remote Downloads](#remote-downloads)
   - [List Downloads](#get-apiserversserverfilespull)
   - [Start Download](#post-apiserversserverfilespull)
   - [Cancel Download](#delete-apiserversserverfilespulldownload)
7. [Quick Reference](#quick-reference)

---

## Authentication

All protected API endpoints require Bearer token authentication:

```
Authorization: Bearer <token>
```

The token is configured in `/etc/pelican/config.yml` and must match the token stored in the Panel.

### Response Format

**Success Response:**

```json
{
  "data": { ... }
}
```

**Error Response:**

```json
{
  "error": "Human-readable error message",
  "request_id": "uuid-for-tracking"
}
```

### HTTP Status Codes

| Code | Meaning                                 |
| ---- | --------------------------------------- |
| 200  | Success                                 |
| 202  | Accepted (async processing)             |
| 204  | No Content (success, no body)           |
| 400  | Bad Request                             |
| 401  | Unauthorized (missing token)            |
| 403  | Forbidden (invalid token)               |
| 404  | Not Found                               |
| 409  | Conflict                                |
| 422  | Unprocessable Entity (validation error) |
| 500  | Internal Server Error                   |

---

## File Operations

### GET /api/servers/:server/files/contents

Read file contents.

**Authentication:** Required

**Path Parameters:**
| Parameter | Type | Description |
|-----------|------|-------------|
| `server` | string | Server UUID |

**Query Parameters:**
| Parameter | Type | Description |
|-----------|------|-------------|
| `file` | string | File path (required) |
| `download` | string | If present, returns as attachment |

**Response Headers:**

```
X-Mime-Type: text/plain
Content-Length: 1234
Content-Disposition: attachment; filename="config.yml"
```

**Response:** Raw file content

**Errors:**

- 404: File not found
- 400: File is a directory or named pipe

**Example Request:**

```bash
curl -X GET "https://wings.example.com/api/servers/{server}/files/contents?file=/server.properties" \
  -H "Authorization: Bearer <token>"
```

---

### GET /api/servers/:server/files/list-directory

List directory contents.

**Authentication:** Required

**Path Parameters:**
| Parameter | Type | Description |
|-----------|------|-------------|
| `server` | string | Server UUID |

**Query Parameters:**
| Parameter | Type | Description |
|-----------|------|-------------|
| `directory` | string | Directory path (required) |

**Response:**

```json
[
  {
    "name": "server.jar",
    "mode": "-rw-r--r--",
    "mode_bits": "0644",
    "size": 45678912,
    "is_file": true,
    "is_symlink": false,
    "mimetype": "application/java-archive",
    "created_at": "2024-01-15T10:30:00Z",
    "modified_at": "2024-01-15T10:30:00Z"
  },
  {
    "name": "world",
    "mode": "drwxr-xr-x",
    "mode_bits": "0755",
    "size": 4096,
    "is_file": false,
    "is_symlink": false,
    "mimetype": "inode/directory",
    "created_at": "2024-01-15T10:30:00Z",
    "modified_at": "2024-01-15T10:30:00Z"
  }
]
```

**File Object Properties:**

| Property | Type | Description |
|----------|------|-------------|
| `name` | string | File or directory name |
| `mode` | string | Unix-style permission string |
| `mode_bits` | string | Octal permission bits |
| `size` | integer | Size in bytes |
| `is_file` | boolean | True if regular file |
| `is_symlink` | boolean | True if symbolic link |
| `mimetype` | string | MIME type of the file |
| `created_at` | string | ISO 8601 creation timestamp |
| `modified_at` | string | ISO 8601 modification timestamp |

**Example Request:**

```bash
curl -X GET "https://wings.example.com/api/servers/{server}/files/list-directory?directory=/" \
  -H "Authorization: Bearer <token>"
```

---

### POST /api/servers/:server/files/write

Write file contents.

**Authentication:** Required

**Path Parameters:**
| Parameter | Type | Description |
|-----------|------|-------------|
| `server` | string | Server UUID |

**Query Parameters:**
| Parameter | Type | Description |
|-----------|------|-------------|
| `file` | string | File path (required) |

**Request Headers:**

```
Content-Length: 1234
```

**Request Body:** Raw file content

**Response:** 204 No Content

**Example Request:**

```bash
curl -X POST "https://wings.example.com/api/servers/{server}/files/write?file=/config.yml" \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: text/plain" \
  -d "server-port=25565\nmax-players=20"
```

---

### PUT /api/servers/:server/files/rename

Rename or move files.

**Authentication:** Required

**Path Parameters:**
| Parameter | Type | Description |
|-----------|------|-------------|
| `server` | string | Server UUID |

**Request Body:**

```json
{
  "root": "/",
  "files": [
    {
      "from": "old-name.txt",
      "to": "new-name.txt"
    }
  ]
}
```

**Request Body Properties:**

| Property | Type | Description |
|----------|------|-------------|
| `root` | string | Root directory for relative paths |
| `files` | array | Array of rename operations |
| `files[].from` | string | Source file name (relative to root) |
| `files[].to` | string | Destination file name (relative to root) |

**Response:** 204 No Content

**Errors:**

- 422: No files provided
- 400: Destination exists

**Example Request:**

```bash
curl -X PUT "https://wings.example.com/api/servers/{server}/files/rename" \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "root": "/",
    "files": [
      {"from": "old-config.yml", "to": "config.yml"}
    ]
  }'
```

---

### POST /api/servers/:server/files/copy

Copy a file.

**Authentication:** Required

**Path Parameters:**
| Parameter | Type | Description |
|-----------|------|-------------|
| `server` | string | Server UUID |

**Request Body:**

```json
{
  "location": "/path/to/file.txt"
}
```

**Response:** 204 No Content

Creates a copy with "\_copy" suffix. For example, `file.txt` becomes `file_copy.txt`.

**Example Request:**

```bash
curl -X POST "https://wings.example.com/api/servers/{server}/files/copy" \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"location": "/server.properties"}'
```

---

### POST /api/servers/:server/files/delete

Delete files and/or directories.

**Authentication:** Required

**Path Parameters:**
| Parameter | Type | Description |
|-----------|------|-------------|
| `server` | string | Server UUID |

**Request Body:**

```json
{
  "root": "/",
  "files": ["file1.txt", "folder1"]
}
```

**Request Body Properties:**

| Property | Type | Description |
|----------|------|-------------|
| `root` | string | Root directory for relative paths |
| `files` | array | Array of file/directory names to delete |

**Response:** 204 No Content

**Example Request:**

```bash
curl -X POST "https://wings.example.com/api/servers/{server}/files/delete" \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "root": "/",
    "files": ["old-world", "backup.zip"]
  }'
```

---

### POST /api/servers/:server/files/create-directory

Create a directory.

**Authentication:** Required

**Path Parameters:**
| Parameter | Type | Description |
|-----------|------|-------------|
| `server` | string | Server UUID |

**Request Body:**

```json
{
  "name": "new-folder",
  "path": "/parent/path"
}
```

**Request Body Properties:**

| Property | Type | Description |
|----------|------|-------------|
| `name` | string | Name of the new directory |
| `path` | string | Parent directory path |

**Response:** 204 No Content

**Example Request:**

```bash
curl -X POST "https://wings.example.com/api/servers/{server}/files/create-directory" \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"name": "plugins", "path": "/"}'
```

---

## Archive Operations

### POST /api/servers/:server/files/compress

Compress files into an archive.

**Authentication:** Required

**Path Parameters:**
| Parameter | Type | Description |
|-----------|------|-------------|
| `server` | string | Server UUID |

**Request Body:**

```json
{
  "root": "/",
  "files": ["file1.txt", "folder1"],
  "name": "archive",
  "extension": "tar.gz"
}
```

**Request Body Properties:**

| Property | Type | Description |
|----------|------|-------------|
| `root` | string | Root directory for relative paths |
| `files` | array | Array of file/directory names to compress |
| `name` | string | Output archive name (without extension) |
| `extension` | string | Archive format extension |

**Supported Extensions:**

| Extension | Format |
|-----------|--------|
| `zip` | ZIP archive |
| `tar.gz` | Gzip compressed tar |
| `tar.bz2` | Bzip2 compressed tar |
| `tar.xz` | XZ compressed tar |

**Response:** File stat object of the created archive

```json
{
  "name": "archive.tar.gz",
  "mode": "-rw-r--r--",
  "mode_bits": "0644",
  "size": 12345678,
  "is_file": true,
  "is_symlink": false,
  "mimetype": "application/gzip",
  "created_at": "2024-01-15T10:30:00Z",
  "modified_at": "2024-01-15T10:30:00Z"
}
```

**Errors:**

- 409: Insufficient disk space

**Example Request:**

```bash
curl -X POST "https://wings.example.com/api/servers/{server}/files/compress" \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "root": "/",
    "files": ["world", "plugins"],
    "name": "server-backup",
    "extension": "tar.gz"
  }'
```

---

### POST /api/servers/:server/files/decompress

Extract an archive.

**Authentication:** Required

**Path Parameters:**
| Parameter | Type | Description |
|-----------|------|-------------|
| `server` | string | Server UUID |

**Request Body:**

```json
{
  "root": "/extract/to",
  "file": "/path/to/archive.tar.gz"
}
```

**Request Body Properties:**

| Property | Type | Description |
|----------|------|-------------|
| `root` | string | Destination directory for extraction |
| `file` | string | Path to the archive file |

**Supported Formats:**

- tar
- tar.gz (gzip)
- tar.bz2 (bzip2)
- tar.xz (xz)
- zip

**Response:** 204 No Content

**Example Request:**

```bash
curl -X POST "https://wings.example.com/api/servers/{server}/files/decompress" \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "root": "/",
    "file": "/server-backup.tar.gz"
  }'
```

---

## Permissions

### POST /api/servers/:server/files/chmod

Change file permissions.

**Authentication:** Required

**Path Parameters:**
| Parameter | Type | Description |
|-----------|------|-------------|
| `server` | string | Server UUID |

**Request Body:**

```json
{
  "root": "/",
  "files": [
    {
      "file": "script.sh",
      "mode": "0755"
    }
  ]
}
```

**Request Body Properties:**

| Property | Type | Description |
|----------|------|-------------|
| `root` | string | Root directory for relative paths |
| `files` | array | Array of chmod operations |
| `files[].file` | string | File name (relative to root) |
| `files[].mode` | string | Octal permission mode |

**Common Permission Modes:**

| Mode | Description |
|------|-------------|
| `0644` | Owner read/write, others read |
| `0755` | Owner read/write/execute, others read/execute |
| `0600` | Owner read/write only |
| `0700` | Owner read/write/execute only |

**Response:** 204 No Content

**Example Request:**

```bash
curl -X POST "https://wings.example.com/api/servers/{server}/files/chmod" \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "root": "/",
    "files": [
      {"file": "start.sh", "mode": "0755"}
    ]
  }'
```

---

## Search

### GET /api/servers/:server/files/search

Search for files by name pattern.

**Authentication:** Required

**Path Parameters:**
| Parameter | Type | Description |
|-----------|------|-------------|
| `server` | string | Server UUID |

**Query Parameters:**
| Parameter | Type | Description |
|-----------|------|-------------|
| `directory` | string | Root directory to search (required) |
| `pattern` | string | Search pattern, min 3 chars (required) |

**Pattern Syntax:**

| Pattern | Description |
|---------|-------------|
| `*` | Wildcard (any characters) |
| `?` | Single character |
| `.js` | File extension |
| `config` | Substring matching (case-insensitive) |

**Examples:**
- `*.yml` - All YAML files
- `config*` - Files starting with "config"
- `*.log` - All log files
- `server` - Files containing "server" in the name

**Response:**

```json
[
  {
    "name": "config.json",
    "path": "/plugins/config.json"
  },
  {
    "name": "server-config.yml",
    "path": "/server-config.yml"
  }
]
```

**Blacklisted Directories:**

The following directories are automatically excluded from search:
- node_modules
- .git
- .wine
- appcache
- depotcache
- vendor

**Configuration:**

Search behavior can be customized in config.yml:

```yaml
search:
  blacklisted_dirs:
    - node_modules
    - .git
    - .wine
    - appcache
    - depotcache
    - vendor
  max_recursion_depth: 8
```

**Example Request:**

```bash
curl -X GET "https://wings.example.com/api/servers/{server}/files/search?directory=/&pattern=*.yml" \
  -H "Authorization: Bearer <token>"
```

---

## Remote Downloads

### GET /api/servers/:server/files/pull

List in-progress remote downloads.

**Authentication:** Required

**Middleware:** RemoteDownloadEnabled (checks `api.disable_remote_download`)

**Path Parameters:**
| Parameter | Type | Description |
|-----------|------|-------------|
| `server` | string | Server UUID |

**Response:**

```json
[
  {
    "identifier": "download-uuid",
    "url": "https://example.com/file.zip",
    "progress": 45.5
  }
]
```

**Response Properties:**

| Property | Type | Description |
|----------|------|-------------|
| `identifier` | string | Unique download identifier |
| `url` | string | Source URL being downloaded |
| `progress` | number | Download progress percentage (0-100) |

**Example Request:**

```bash
curl -X GET "https://wings.example.com/api/servers/{server}/files/pull" \
  -H "Authorization: Bearer <token>"
```

---

### POST /api/servers/:server/files/pull

Start remote file download.

**Authentication:** Required

**Middleware:** RemoteDownloadEnabled

**Path Parameters:**
| Parameter | Type | Description |
|-----------|------|-------------|
| `server` | string | Server UUID |

**Request Body:**

```json
{
  "url": "https://example.com/file.zip",
  "root": "/download/to",
  "file_name": "custom_name.zip",
  "use_header": false,
  "foreground": false
}
```

**Request Body Properties:**

| Property | Type | Default | Description |
|----------|------|---------|-------------|
| `url` | string | (required) | URL to download from |
| `root` | string | `/` | Destination directory |
| `file_name` | string | (from URL) | Custom file name |
| `use_header` | boolean | false | Use Content-Disposition header for filename |
| `foreground` | boolean | false | Wait for download to complete |

**Response (background):** 202 Accepted

```json
{
  "identifier": "download-uuid"
}
```

**Response (foreground):** 200 OK with file stats

```json
{
  "name": "file.zip",
  "mode": "-rw-r--r--",
  "mode_bits": "0644",
  "size": 12345678,
  "is_file": true,
  "is_symlink": false,
  "mimetype": "application/zip",
  "created_at": "2024-01-15T10:30:00Z",
  "modified_at": "2024-01-15T10:30:00Z"
}
```

**Limits:**
- Maximum 3 concurrent downloads per server

**Configuration:**

```yaml
api:
  disable_remote_download: false
  remote_download:
    max_redirects: 10
```

**Example Request:**

```bash
curl -X POST "https://wings.example.com/api/servers/{server}/files/pull" \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "url": "https://papermc.io/api/v2/projects/paper/versions/1.20.4/builds/463/downloads/paper-1.20.4-463.jar",
    "root": "/",
    "file_name": "server.jar"
  }'
```

---

### DELETE /api/servers/:server/files/pull/:download

Cancel remote download.

**Authentication:** Required

**Path Parameters:**
| Parameter | Type | Description |
|-----------|------|-------------|
| `server` | string | Server UUID |
| `download` | string | Download identifier |

**Response:** 204 No Content

**Example Request:**

```bash
curl -X DELETE "https://wings.example.com/api/servers/{server}/files/pull/{download-uuid}" \
  -H "Authorization: Bearer <token>"
```

---

## Quick Reference

| Method | Endpoint | Description |
| ------ | -------- | ----------- |
| GET | /api/servers/:server/files/contents | Read file |
| GET | /api/servers/:server/files/list-directory | List directory |
| POST | /api/servers/:server/files/write | Write file |
| PUT | /api/servers/:server/files/rename | Rename/move |
| POST | /api/servers/:server/files/copy | Copy file |
| POST | /api/servers/:server/files/delete | Delete files |
| POST | /api/servers/:server/files/create-directory | Create directory |
| POST | /api/servers/:server/files/compress | Compress files |
| POST | /api/servers/:server/files/decompress | Extract archive |
| POST | /api/servers/:server/files/chmod | Change permissions |
| GET | /api/servers/:server/files/search | Search files |
| GET | /api/servers/:server/files/pull | List downloads |
| POST | /api/servers/:server/files/pull | Start download |
| DELETE | /api/servers/:server/files/pull/:download | Cancel download |

---

_Documentation for Pelican Wings File Management API. For complete documentation, see [DOCUMENTATION.md](./DOCUMENTATION.md)._

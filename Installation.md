
# The different components

For any clustta client to work, there are three crucial parts
1. [The Global/Private Authentication server](https://github.com/eaxum/clustta-server)
2. The Studio server (This)
3. [The Client application](https://github.com/eaxum/clustta-client)

Make reference to the other repositories for specific guides to each.



## The Studio Server (This repository)
This is the private instance of any Clustta studio server. All the studio projects are stored here and the machine's IP is registered on the Clustta Global server.
When a client attempts to reach this server, it first accesses the global server which then routes it to this machine's IP address.

If you are authenticating against your own private server, the client will access it directly using the IP address/url you provide. See [The Private Authentication server](https://github.com/eaxum/clustta-private-auth)

<br>

# Quick install: Setting up Clustta studio on your machine

## One-line install (recommended)

The fastest way to get Clustta Studio running. This script will install Docker if needed, download the compose file, walk you through configuration, and start the server:

```bash
curl -fsSL https://raw.githubusercontent.com/eaxum/clustta-studio/main/install.sh | bash
```

### Install options

| Flag | Description |
|------|-------------|
| `--private` | Skip Clustta Cloud setup (standalone mode) |
| `--traefik` | Include Traefik reverse proxy with auto-TLS |
| `--dir PATH` | Custom install directory (default: `~/clustta-studio`) |
| `--version VER` | Pin a specific image version (default: `latest`) |

Example — private mode with Traefik:
```bash
curl -fsSL https://raw.githubusercontent.com/eaxum/clustta-studio/main/install.sh | bash -s -- --private --traefik
```

After installation, manage your server with:
```bash
cd ~/clustta-studio
docker compose logs -f      # view logs
docker compose restart       # restart
docker compose down          # stop
docker compose pull && docker compose up -d   # update
```

<br>

## Manual install with Docker

If you prefer to set things up manually, or the install script doesn't support your OS:

### 1. Install Docker

If Docker isn't already installed, install it and all necessary dependencies. Copy and paste the following script into the terminal:

```bash
sudo apt update && sudo apt upgrade -y && sudo apt install -y apt-transport-https ca-certificates curl software-properties-common && curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo apt-key add - && sudo add-apt-repository "deb [arch=amd64] https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable" && sudo apt update && sudo apt install -y docker-ce && sudo systemctl enable docker && sudo usermod -aG docker $USER && sudo apt autoremove -y
```

After installation, add your user to the docker group and re-login:
```bash
sudo usermod -aG docker $USER
newgrp docker
```

### 2. Set up the project directory

```bash
mkdir clustta-studio && cd clustta-studio
```

Download the compose file (choose one):

**Standalone** (no reverse proxy — use if you have your own nginx/Caddy, or on a LAN):
```bash
curl -fsSL https://raw.githubusercontent.com/eaxum/clustta-studio/main/deploy/docker-compose.yml -o docker-compose.yml
```

**With Traefik** (includes reverse proxy and optional TLS):
```bash
curl -fsSL https://raw.githubusercontent.com/eaxum/clustta-studio/main/deploy/docker-compose.traefik.yml -o docker-compose.yml
```

### 3. Configure environment

Create a `.env` file:

```bash
DATA_FOLDER=./data
PROJECTS_FOLDER=./projects
STUDIO_USERS_DB=/var/data/studio_users.db
SESSION_DB=/var/data/sessions.db
PRIVATE=true
```

If connecting to Clustta Cloud, set `PRIVATE=false` and add:
```bash
CLUSTTA_STUDIO_API_KEY=YourStudioKey
CLUSTTA_SERVER_NAME=YourStudioName
CLUSTTA_SERVER_URL=http://your-host-ip/clustta
```

See [Creating and accessing a studio](#creating-and-accessing-a-studio) for how to obtain the `StudioKey`.

### 4. Start the server

```bash
mkdir -p data projects
docker compose up -d
```

<br>

> ⚠️ NOTE
>
> Ensure ports `80` and `443` (if using Traefik) or `7774` (if standalone) are open on your server.

> ⚠️ NOTE
>
> You may need to set permissions on the projects directory:
```bash
sudo chmod a+w ./projects/
```

<br>
<br>
<br>

# Creating and accessing a studio
When setting up a studio, if your users will be authenticating against Clustta's global database, get the `StudioKey` by logging in to the client application and clicking the 'Create Studio' button.

Follow the propmts on the UI to complete the setup

<br>
<br>
<br>

# Development: Setting up and running the environment

To run the development environment, we need a number of dependencies:
- Go
- Wails3
- Air
- Protocol Buffers (Protoc & PBJS)


## Go
Install `Go` for your target OS following instructions from the [Official Documentation](https://go.dev/doc/install)

## Wails3
Install the Wails CLI using Go Modules, run the following command:
Clustta currently uses v3.0.0-alpha.9

```bash
go install -v github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0-alpha.9
```

## Air
To run the development server, we will use `Air`. Install using Go Modules:
```bash
go install github.com/air-verse/air@latest
```

## Protocol buffers
To transmit data efficiently, Clustta uses Protocol Buffers to serialize data for transmission.

We are using  `protoc` to generate the data for Go.
Install it like so:

Protoc via Go Modules:
```bash
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest 
```

<br>

Whenever you update `internal\repository\schema.proto` or `internal\repository\proto_helpers.go` , generate the files like so:

```bash
protoc --go_out=. .\internal\repository\schema.proto 
```

<br>

# Running the development environment


## Development Server and Studio
To run the development studio server, run

```bash
make studio
```
Ensure that the development server from `clustta-server` is already running else this will fail.

You will need a `StudioKey` for the `studio` to run successfully. See [Getting a StudioKey](#getting-a-studiokey-for-development).
<br>






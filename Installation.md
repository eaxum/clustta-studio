
# Quick install: Setting up Clustta studio on your machine

This is the private instance of any Clustta studio server. All the studio projects are stored here and the machine's IP is registered on the Clustta Global server


If docker isnt already installed, install docker and all of it's necessary dependencies on the new machine. Copy and paste the following script into the termial. It will:
- Update system packages
- Install Docker CE (Community Edition)
- Install latest Docker Compose
- Add current user to docker group
- Enable Docker to start on boot
- Clean up unnecessary packages

```bash
sudo apt update && sudo apt upgrade -y && sudo apt install -y apt-transport-https ca-certificates curl software-properties-common && curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo apt-key add - && sudo add-apt-repository "deb [arch=amd64] https://download.docker.com/linux/ubuntu $(lsb_release -cs) stable" && sudo apt update && sudo apt install -y docker-ce && sudo systemctl enable docker && sudo usermod -aG docker $USER && mkdir -p ~/.docker/cli-plugins/ && curl -SL "https://github.com/docker/compose/releases/latest/download/docker-compose-linux-x86_64" -o ~/.docker/cli-plugins/docker-compose && chmod +x ~/.docker/cli-plugins/docker-compose && sudo apt autoremove -y && echo 
```

After the script completes, you must log out and log back in for Docker group membership to take effect.

### Setting up the variables
Create a directory to place the `docker-compose.yml` and `.env` files

```bash
mkdir clustta-docker
```
Create a `docker-compose.yml`. Copy and paste the contents of `cmd\studio_server\compose.yml` into it.

Create the `.env` file and add the following parameters:

```bash
DATA_FOLDER=/home/server-user-name/data/
PROJECTS_FOLDER=/home/server-user-name/projects/
CLUSTTA_STUDIO_API_KEY=StudioKey
CLUSTTA_SERVER_NAME=StudioName
CLUSTTA_SERVER_URL=http://host-ip/clustta
```
`server-user-name` : the username to the host machine.

`StudioKey` : the key generated earlier in [Getting a StudioKey](#getting-a-studiokey).

`StudioName` : The registered studio name exactly as formatted on creation.

`host-ip` : The IP address of the host machine.


Compose the docker file
```bash
docker compose up -d
```

<br>


> ⚠️ NOTE
>
> Ensure the ports `80` and `443` are enabled on your server host else the client can't reach it.

> ⚠️ NOTE
>
> You also need to change permissions so clustta can write to the projects directory 

```bash
sudo chmod a+w ./projects/
```

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

# The different components

For any clustta client to work, there are three crucial parts
1. [The Global/Authentication server](https://github.com/eaxum/clustta-server)
2. The Studio server (This)
3. [The Client application](https://github.com/eaxum/clustta-client)

Make reference to the other repositories for specific guides to each.

### Getting a StudioKey(Global)
When registering a studio, get the `StudioKey` by running the binary providing two arguments:
- The `clusta.db` path
- The `StudioName` (The studio must have been registered from the dashboard beforehand and must match the exact format - casing and spacing).

```bash
./studio-key /path/to/clustta.db StudioName
```

### Getting a StudioKey for Development
When registering a studio for development, get the `StudioKey` by running the go file at `cmd\studio_key\main.go` providing two arguments:
- The `clusta.db` path
- The `StudioName` (The studio must have been registered from the dashboard beforehand and must match the exact format - casing and spacing).

```bash
go run ./cmd/studio_key ./data/clustta.db StudioName
```

This will print the `StudioKey` to the terminal. Copy and save it for use in [Setting up the variables](#setting-up-the-variables).

### Local studio config
After getting a studio key, you need to update the `server_name` and `studio_api_key` in the `studio_config.json`.

## The Studio Server (This repository)
This is the private instance of any Clustta studio server. All the studio projects are stored here and the machine's IP is registered on the Clustta Global server.

When a client attempts to reach this server, it first accesses the global server which then routes it to this machine's IP address.

### Building and pushing the Docker image
For simplicity, we can deploy using a docker container from the built GO application:

Build the docker image and tag it with the appropriate version
```bash
docker build -f .\cmd\studio_server\Dockerfile -t registry_name/clustta:latest -t registry_name/clustta:x.x.xx .
```

Login to docker hub
```bash
docker login -u registry_name -p password
```

Push the tagged image so it's accessible from any machine on deployment
```bash
docker push registry_name/clustta:latest
```








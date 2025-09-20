
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

`StudioKey` : the key generated in [Creating and accessing a studio](#creating-and-accessing-a-studio)

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






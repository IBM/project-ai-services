package main

import "github.com/project-ai-services/ai-services/cmd/ai-services/cmd"

// ai-services completion bash|zsh|fish|powershell
// ai-services --version

// ai-services --help

// ai-services application templates (Multiple application templates Eg:- RAG)
/*
	- Reads the path from the assets
	- Reads all the application template names from path: assets/applications/<AppTemplateName>/templates/*.yaml.tmpl
	- Prints all the Application template names in the terminal
*/

// ai-services application create <name> --template-name "<application_template_name>"
// Eg:- ai-services application create it-desk --template-name "RAG"
/*
	Args
		- name: Application Name (Required)

	Flags
		- template-name: Name of the application template (Required)

	- First check if the application provided actually exists (Loop over the assets/applications and see if the application exists)
	- Read all the templates present under that application dir
	- Apply templating params and do kube-play to deploy all the pods onto the podman
	- Wait until all the pods are RUNNING/READY
	- Display the message in the terminal to the user to instruct them for data ingestion (once created/ pods are READY)
	- Display Chatbot UI url in the terminal

	Considerations
	- User provides the name of the application to deploy
	- Adding AppName as namespace label within the pod template files
*/

// ai-services application ps <name>
/*
	Args
		- name: Application Name (Optional)

	- By default lists all the applications. Use {name} to get info about the specific application.
	- Do podman List Pods to fetch all the pods currently being deployed with filters
*/

// ai-services application delete <name>
/*
	Args
		- name: Application Name (Required)

	- User provides the name of the application to delete the application.
	- List all the pods belonging to the particular application.
	- Loop over each of the pods and call Podman delete with forced set to 'True'
*/

// ai-services application create application it-desk --template-name "RAG"
// application create workflow
// deploy vllm
// deploy opensearch
// deploy ui
// deploy ....
// deploy ingest job

// ai-services application start it-desk --pod-name ingest
// Running serices
// deploy ingest job

// ai-services application images ls --template-name "RAG"

func main() {
	cmd.Execute()
}

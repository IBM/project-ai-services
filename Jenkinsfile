pipeline {
    agent { label 'ai-services-4-spyre-card-1' }

    environment {
        PROJECT_NAME = 'project-ai-services'
        AI_SERVICES_DIR = 'ai-services'
        AI_SERVICES_BINARY = './bin/ai-services'

        // Holding pipeline for configured minutes, to allow user to complete testing
        MAX_APP_RUN_TIME_IN_MINS = '20'
    }

    // Using options to allow one deployment at any given point of time.
    options {
        disableConcurrentBuilds()
    }

    stages {
        stage('Validate parameters') {
            steps {
                script {
                    if (!params.CHECKOUT.trim()) {
                        error('PR number or commit hash must be provided')
                    }
                }
            }
        }

        stage('Checkout Code') {
            steps {
                script {
                    deleteDir()
                    sh "pwd"
                    repoCheckout(params.CHECKOUT)
                }
            }
        }

        stage('Build AI Services Binary') {
            steps {
                sh '''
                    cd ${PROJECT_NAME}/${AI_SERVICES_DIR}
                    make build
                    ${AI_SERVICES_BINARY} --version
                '''
            }
        }

        // Delete app deployed by cicd pipeline
        stage('Delete CICD App') {
            steps{
                script {
                    deleteCicdApp()
                }
            }
        }

        // Build image locally, based on selection made by user
        stage('Build Selected Image') {
            steps {
                script {
                    buildAppImage(params.APP)
                }
            }
        }

        // Deploy selected application
        stage('Deployment') {
            steps {
                sh '''
                    cd ${PROJECT_NAME}/${AI_SERVICES_DIR}
                    ${AI_SERVICES_BINARY} application create ${APP}-cicd -t ${APP}
                '''
            }
        }

        // Ingest Docsument
        stage('Ingest DOC') {
            steps {
                script {
                unstash 'INGEST_DOC_FILE'
                    
                sh '''
                    mv INGEST_DOC_FILE /var/lib/ai-services/applications/${APP}-cicd/docs/doc.pdf
                    echo "ingest DOC"
                    cd ${PROJECT_NAME}/${AI_SERVICES_DIR}
                    ${AI_SERVICES_BINARY} application start ${APP}-cicd --pod=${APP}-cicd--ingest-docs -y
                '''
                }
                
            }
        }

        stage('Test Deployment') {
            steps{
                script{
                    // Polling to check if app is deleted
                    for (int i = 0; i < env.MAX_APP_RUN_TIME_IN_MINS.toInteger() ; i++) {
                        def appName = runningAppName()
                        if (appName.isEmpty()) {
                            break
                        }
                        println "Iteration number ${i}, waiting for 60s"
                        sh 'sleep 60s'
                    }
                }
            }
        }
    }
}

// Returning app name which is running in the machine
def runningAppName() {
    String appName = ''
    dir("${env.PROJECT_NAME}/${env.AI_SERVICES_DIR}") {
        def apps = sh(
            script: './bin/ai-services application ps 2>&1',
            returnStdout: true
        ).trim()
        def outputLines = apps.readLines()
        if (outputLines.size() > 2 ) {
            appName = outputLines[2].split()[0]
            echo "${appName}"
        }
    }
    return appName
}

def repoCheckout(String branch) {
    sh 'git clone https://github.com/IBM/project-ai-services.git'
    dir("${PROJECT_NAME}") {
        if (branch ==~ /^\d+$/) {
            println "Checking out to ${branch} PR number"
            sh """
                git fetch origin pull/${branch}/head:pr-${branch}
                git checkout pr-${branch}
            """
        } else {
            println "Checking out to ${branch} commit hash"
            sh "git rev-parse --verify ${branch}"
            sh "git checkout ${branch}"
        }
    }
}


// Delete application deployed via CI/CD pipeline
def deleteCicdApp() {
    String appName = runningAppName()
    dir("${env.PROJECT_NAME}/${env.AI_SERVICES_DIR}") {
        if (appName.contains('cicd')) {
            println "Cleaning up ${appName} which is deployed by pipelines."
            sh "./bin/ai-services application delete ${appName} -y"
        }
    }
}

// Build image for app, as per user selection
def buildAppImage(String appName) {
    String imageName = ""
    String containerFilePath = ""
    String jsonPath = ""
    
    if (appName == 'rag-dev') {
        String images = params.BUILD_IMAGE
        println "Params: ${params.BUILD_IMAGE}"
        if (images) {
            String[] selectedImage = params.BUILD_IMAGE.split(',')
            for (image in selectedImage) {
                if (image == 'BUILD_RAG_UI') {
                    imageName = 'rag-ui'
                    containerFilePath = 'spyre-rag/ui'
                    jsonPath = '.ui.image'
                }
                if (image == 'BUILD_RAG_BACKEND') {
                    imageName = 'rag'
                    containerFilePath = 'spyre-rag/src'
                    jsonPath = '.backend.image'
                }
                String imageVal = buildImage(imageName, containerFilePath)
                updateYamlFile(params.APP, imageVal, jsonPath)
            }
        } else {
            println 'Images are not built locally for deployment.'
        }
    } else {
        error("Selected ${appName} not supported.")
    }
}

def buildImage(String imageName, String containerFilePath) {
    String localRegistry = 'localhost'
    String imagePath = ""
    dir(env.PROJECT_NAME) {
        sh 'git rev-parse --short HEAD'
        dir(containerFilePath) {
            // Build image
            sh "make build REGISTRY=${localRegistry}"

            // Get the tag from Makefile
            def tag = sh(script: "make image-tag REGISTRY=${localRegistry}", returnStdout: true).trim()
            imagePath = "${localRegistry}/${imageName}:${tag}"
            echo "Backend image set to ${imagePath}"
        }
    }
    return imagePath
}

// Method to update local image in the yaml file
// so that deployment of local image is done
def updateYamlFile(String appName,String imageValue, String overridePath) {
    dir(env.PROJECT_NAME) {
        String valuesFile = "ai-services/assets/applications/${appName}/values.yaml"
        if (!fileExists(valuesFile)) {
            error("no values.yaml for ${appName}")
            return
        }
        sh "yq e '${overridePath} = \"${imageValue}\"' -i ${valuesFile}"
        println "Values.yaml is updated in ${appName} for parameter ${overridePath} with value ${imageValue}"
    }
}

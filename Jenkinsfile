pipeline {
    agent { label 'ai-services-4-spyre-card-1' }

    environment {
        PROJECT_NAME = 'project-ai-services'
        AI_SERVICES_DIR = 'ai-services'
        AI_SERVICES_BINARY = './bin/ai-services'

        RAG_APP_NAME = 'rag-dev'
        // Holding pipeline for configured minutes, to allow user to complete testing
        MAX_APP_RUN_TIME_IN_MINS = '2'
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
                    repoCheckout(params.CHECKOUT)
                }
            }
        }

        stage('Detect Changes') {
            steps {
                script {
                    def ragAPP = isFileUpdated('spyre').toBoolean()
                    if (ragAPP) {
                        env.DEPLOY_APP = "${env.RAG_APP_NAME}"
                        println 'There is an update in RAG app'
                    } else {
                        println 'No updates found in the supported apps'

                        // Setting default values, will remove it
                        env.DEPLOY_APP = "${env.RAG_APP_NAME}"
                    }
                    env.APP_NAME = "${env.DEPLOY_APP}-cicd"
                    println "App going to deploy ${env.APP_NAME}"
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
        stage('Build Images') {
            steps {
                script {
                    if (env.DEPLOY_APP == env.RAG_APP_NAME) {
                        buildAppImage(env.RAG_APP_NAME)
                    } else {
                        println 'Skip image build'
                    }
                    
                }
            }
        }

        // Deploy selected application
        stage('Deployment') {
            steps {
                sh '''
                    cd ${PROJECT_NAME}/${AI_SERVICES_DIR}
                    ${AI_SERVICES_BINARY} application create ${APP_NAME} -t ${DEPLOY_APP}
                '''
            }
        }

        // Ingest Docsument
        stage('Ingest DOC') {
            steps {
                script {
                    
                sh '''
                    cp -r /root/cicd-app-doc/${APP_NAME}/* /var/lib/ai-services/applications/${APP_NAME}/docs/
                    echo "Ingest DOC"
                    cd ${PROJECT_NAME}/${AI_SERVICES_DIR}
                    ${AI_SERVICES_BINARY} application start ${APP_NAME} --pod=${APP_NAME}--ingest-docs -y
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

def isFileUpdated(String path) {
    dir("${PROJECT_NAME}") {
        sh 'git fetch origin main'
        def changedFiles = sh(
            script: 'git diff --name-only origin/main',
            returnStdout: true
        ).trim().split('\n')
        println "${changedFiles}"
        return changedFiles.any{ it.startsWith(path)}
    }
    return false
}

// Build image for app, as per user selection
def buildAppImage(String appName) {
    if (appName == env.RAG_APP_NAME) {
        def imageMap = [
            [imageName: "rag-ui", filePath: "spyre-rag/ui", jsonPath: ".ui.image"],
            [imageName: "rag", filePath: "spyre-rag/src", jsonPath: ".backend.image"]
        ]

        for (imageInfo in imageMap) {
            def isChanged = isFileUpdated(imageInfo.filePath).toBoolean()
            if (isChanged) {
                println "Rebuilding ${imageMap.imageName} for deployment"
                String imageVal = buildImage(imageMap.imageName, imageMap.filePath)
                updateYamlFile(env.RAG_APP_NAME, imageVal, imageMap.jsonPath)
            }else {
                println "No changes in ${imageInfo.imageName} image"
            }
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
            sh "make build REGISTRY=${localRegistry}"

            def tag = sh(script: "make image-tag REGISTRY=${localRegistry}", returnStdout: true).trim()
            imagePath = "${localRegistry}/${imageName}:${tag}"
            println "Successfully built ${imagePath} on the machine"
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

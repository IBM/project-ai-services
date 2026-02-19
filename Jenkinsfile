pipeline {
    agent { label 'ai-services-4-spyre-card-1' }

    environment {
        PROJECT_NAME = 'project-ai-services'
        AI_SERVICES_DIR = 'ai-services'
        AI_SERVICES_BINARY = './bin/ai-services'
        SLACK_NOTIFICATION_CHANNEL = '#jenkins-pr-preview'
        RAG_CHAT_BOT_PORT = '4000'
        AI_SERVICES_REPO_URL = 'https://github.com/IBM/project-ai-services.git'

        RAG_APP_NAME = 'rag-dev'
        // Holding pipeline for configured minutes, to allow user to complete testing
        MAX_APP_RUN_TIME_IN_MINS = '120'
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
                    def isRagAppChanged = isFileUpdated('spyre').toBoolean()
                    println "Valued of isRagAppChanged: ${isRagAppChanged}"
                    if (isRagAppChanged) {
                        env.DEPLOY_APP = "${env.RAG_APP_NAME}"
                        println 'Updates are detected in RAG app'
                    } else {
                        println 'No updates found in the supported apps'

                        // Setting default values, will remove it
                        env.DEPLOY_APP = "${env.RAG_APP_NAME}"
                    }
                    env.APP_NAME = "${env.DEPLOY_APP}-cicd"
                    println "${env.APP_NAME} app is going to deployed"
                }
            }
        }

        // Build image locally, if image is modified
        stage('Build Images') {
            steps {
                script {
                    if (env.DEPLOY_APP == env.RAG_APP_NAME) {
                        buildAppImage(env.RAG_APP_NAME)
                    } else {
                        println 'Skipping rebuilding images of the application.'
                    }
                    
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

        stage('Delete CICD App') {
            steps{
                script {
                    deleteCicdApp()
                }
            }
        }

        stage('Deployment') {
            steps {
                script {
                    dir("${env.PROJECT_NAME}/${env.AI_SERVICES_DIR}") {
                        sh "${env.AI_SERVICES_BINARY} application ps"
                        sh "${env.AI_SERVICES_BINARY} application create ${env.APP_NAME} -t ${env.DEPLOY_APP} --params ui.port=${env.RAG_CHAT_BOT_PORT} --skip-validation numa"

                    }
                }
            }
        }

        stage('Ingest Document') {
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
            post {
                success {
                    script {
                        def hostname = sh(script: "hostname -I | awk '{print \$1}'", returnStdout: true).trim()
                        def message = """
                        Application is deployed and document is ingested.
                            *Chat bot URL:* http://${hostname}:${env.RAG_CHAT_BOT_PORT}
                        """
                        sendSlackNotification('good', message)
                    }
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
    post {
        failure {
            script{
                sendSlackNotification('danger', 'Jenkins PR preview pipeline job has been failed with an error.')
            }
        }
        success {
            script {
                sendSlackNotification('good', 'Jenkins PR preview pipeline job has been completed.')
            }
        }
        aborted {
            script{
                sendSlackNotification('warning', 'Jenkins PR preview pipeline job has been aborted.')
            }
        }
    }
}

def sendSlackNotification(String color, String message) {
    def hostname = sh(
        script: "hostname -I | awk '{print \$1}'",
        returnStdout: true
    ).trim()
    
    slackSend(
        channel: "${env.SLACK_NOTIFICATION_CHANNEL}",
        color: "${color}",
        message: """
        ${message}
            *Job Name      :* ${env.JOB_NAME}
            *Build Number  :* ${env.BUILD_NUMBER}
            *Build URL.    :* ${env.BUILD_URL}
            *Host          :* ${hostname}
            *CommitHash/PR :* ${params.CHECKOUT}
        """.stripIndent()
    )
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
    sh "git clone ${env.AI_SERVICES_REPO_URL}"
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
    def isfileChanged = false
    dir("${PROJECT_NAME}") {
        sh 'git fetch origin main'
        def changedFiles = sh(
            script: 'git diff --name-only origin/main',
            returnStdout: true
        ).trim().split('\n')
        println "${changedFiles}"
        isfileChanged = changedFiles.any{ it.startsWith(path)}
    }
    return isfileChanged
}

def buildImage(String imageName, String containerFilePath) {
    String localRegistry = 'localhost'
    String imagePath = ""
    dir(env.PROJECT_NAME) {
        // sh 'git rev-parse --short HEAD'
        dir(containerFilePath) {
            sh "make build REGISTRY=${localRegistry}"

            def tag = sh(script: "make image-tag REGISTRY=${localRegistry}", returnStdout: true).trim()
            imagePath = "${localRegistry}/${imageName}:${tag}"
            println "Successfully built ${imagePath} on the machine"
        }
    }
    return imagePath
}

// Rebuilding modified images in the application
def buildAppImage(String appName) {
    if (appName == env.RAG_APP_NAME) {
        def imageMapList = [
            [imageName: 'rag-ui', filePath: 'spyre-rag/ui', jsonPath: '.ui.image'],
            [imageName: 'rag', filePath: 'spyre-rag/src', jsonPath: '.backend.image']
        ]

        for (imageInfo in imageMapList) {
            def isChanged = isFileUpdated(imageInfo.filePath).toBoolean()
            if (isChanged) {
                println "Rebuilding ${imageInfo.imageName} for deployment"
                String imageVal = buildImage(imageInfo.imageName, imageInfo.filePath)
                updateYamlFile(env.RAG_APP_NAME, imageVal, imageInfo.jsonPath)
            }else {
                println "No changes in ${imageInfo.imageName} image"
            }
        }
    } else {
        error("Selected ${appName} not supported.")
    }
}


// Method to update local image in the yaml file
// so that deployment of local image is done
def updateYamlFile(String appName,String imageValue, String overridePath) {
    dir(env.PROJECT_NAME) {
        String valuesFile = "ai-services/assets/applications/${appName}/podman/values.yaml"
        if (!fileExists(valuesFile)) {
            error("no values.yaml for ${appName}")
            return
        }
        sh "yq e '${overridePath} = \"${imageValue}\"' -i ${valuesFile}"
        println "Values.yaml is updated in ${appName} for parameter ${overridePath} with value ${imageValue}"
    }
}

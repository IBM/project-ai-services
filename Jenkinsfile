pipeline {
    agent { label 'ai-services' }

    parameters {
        string(
            name: 'PR_NUMBER',
            description: 'Pull request number to build'
        )
        stashedFile 'INGEST_DOC_FILE'
    }

    // Using options to allow one deployment at any given point of time.
    options {
        disableConcurrentBuilds()
    }

    stages {

        stage('Validate parameters') {
            steps {
                script {
                    if (!params.PR_NUMBER?.trim()) {
                        error('PR_NUMBER must be provided')
                    }
                }
            }
        }

        stage('Checkout PR') {
            steps {
                sh '''
                cd /root/adarsh
                    rm -rf project-ai-services
                    git clone https://github.com/IBM/project-ai-services.git
                    cd project-ai-services/ai-services
                    git fetch origin pull/${PR_NUMBER}/head
                    git checkout FETCH_HEAD
                '''
            }
        }

        stage('Build') {
            steps {
                sh '''
                    cd /root/adarsh/project-ai-services/ai-services
                    make build
                    ./bin/ai-services --version
                '''
            }
        }

        // Check if there is an existing application
        stage('Machine Status') {
            steps{
                sh "./bin/ai-services application ps"
            }
        }

        stage('Deploy') {
            steps {
                sh '''
                cd /root/adarsh/project-ai-services/ai-services
                    ./bin/ai-services application create cicd-rag-test -t rag-dev
                    podman pod ps
                '''
            }
        }
        stage('Process Ingest Doc file') {
            steps {
                script {
                    unstash 'INGEST_DOC_FILE'
                    
                    sh 'mv INGEST_DOC_FILE /var/lib/ai-services/applications/rag-test/docs/doc.pdf'
                }
            }
        }
        stage('Ingest') {
            steps {
                sh '''
                echo "ingest DOC"
                cd /root/adarsh/project-ai-services/ai-services
                ./bin/ai-services application start rag-test --pod=rag-test--ingest-docs -y
                '''
            }
        }
        stage('Test Your PR') {
            steps {
                sh '''sleep 60s'''
            }
        }

        stage('Cleanup') {
            steps {
                sh '''
                cd /root/adarsh/project-ai-services/ai-services
                ./bin/ai-services application stop rag-test -y
                ./bin/ai-services application delete rag-test -y
                
                podman pod ps
                '''
            }
        }
    }
}

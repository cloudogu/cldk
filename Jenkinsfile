#!groovy
@Library('github.com/cloudogu/ces-build-lib@6.1.0')
import com.cloudogu.ces.cesbuildlib.*

// keep in sync with GOTAG in Makefile
String goVersion = '1.27.3'

String repositoryOwner = 'cloudogu'
String projectName = 'cldk'
String goContainerProjectPath = "/go/src/github.com/${repositoryOwner}/${projectName}/"
String githubCredentialsId = 'sonarqube-gh'

node('docker') {
    timestamps {
        stage('Checkout') {
            checkout scm
        }

        new Docker(this).image("golang:${goVersion}")
                .mountJenkinsUser()
                .inside("--volume ${WORKSPACE}:${goContainerProjectPath} -w ${goContainerProjectPath}") {

                    stage('Build') {
                        make goContainerProjectPath, 'compile'
                        archiveArtifacts "target/${projectName}"
                    }

                    stage('Unit Test') {
                        make goContainerProjectPath, 'unit-test'
                        junit allowEmptyResults: true, testResults: 'target/unit-tests/*-tests.xml'
                    }

                    stage('Static Analysis') {
                        String commitSha = sh(returnStdout: true, script: 'git rev-parse HEAD').trim()
                        withCredentials([usernamePassword(credentialsId: githubCredentialsId, usernameVariable: 'USERNAME', passwordVariable: 'REVIEWDOG_GITHUB_API_TOKEN')]) {
                            withEnv(["CI_PULL_REQUEST=${env.CHANGE_ID}", "CI_COMMIT=${commitSha}", "CI_REPO_OWNER=${repositoryOwner}", "CI_REPO_NAME=${projectName}"]) {
                                make goContainerProjectPath, 'static-analysis'
                            }
                        }
                    }
                }
    }
}

void make(String projectPath, String goal) {
    sh "cd ${projectPath} && make ${goal}"
}

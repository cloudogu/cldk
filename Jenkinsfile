#!groovy
@Library('github.com/cloudogu/ces-build-lib@6.1.0')
import com.cloudogu.ces.cesbuildlib.*

// keep in sync with GOTAG in Makefile
String goVersion = '1.27.4'

String repositoryOwner = 'cloudogu'
String projectName = 'cldk'
String goContainerProjectPath = "/go/src/github.com/${repositoryOwner}/${projectName}/"
String githubCredentialsId = 'sonarqube-gh'
String productionBranch = 'main'

node('docker') {
    Git git = new Git(this, 'cesmarvin')
    git.committerName = 'cesmarvin'
    git.committerEmail = 'cesmarvin@cloudogu.com'
    GitFlow gitflow = new GitFlow(this, git)
    GitHub github = new GitHub(this, git)
    Changelog changelog = new Changelog(this)

    timestamps {
        stage('Checkout') {
            checkout scm
        }

        stage('Check Markdown Links') {
            new Markdown(this).check()
        }

        withGoContainer(goVersion, goContainerProjectPath) {
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

            stage('Go Vuln Check') {
                // fails the build if the code calls a known vulnerable function
                make goContainerProjectPath, 'govulncheck'
            }

            if (gitflow.isReleaseBranch()) {
                // Built before finishing the release, so that a failing build does not leave a pushed tag without release assets.
                stage('Build Release Assets') {
                    make goContainerProjectPath, 'release-assets'
                    archiveArtifacts 'target/dist/*'
                }
            }
        }

        if (gitflow.isReleaseBranch()) {
            // The release branch is named release/v<version>, so the tag and GitHub release are named v<version>.
            String releaseVersion = git.getSimpleBranchName()

            stage('Finish Release') {
                // merges into main and develop, pushes the tag and leaves the tag checked out
                gitflow.finishRelease(releaseVersion, productionBranch)
            }

            stage('Add GitHub Release') {
                String releaseId = github.createReleaseWithChangelog(releaseVersion, changelog, productionBranch)
                if (!releaseId) {
                    return
                }

                String[] assets = sh(returnStdout: true, script: 'ls -1 target/dist').trim().split('\n')
                for (String asset : assets) {
                    github.addReleaseAsset(releaseId, "target/dist/${asset}")
                }
            }
        }
    }
}

void withGoContainer(String goVersion, String goContainerProjectPath, Closure body) {
    new Docker(this).image("golang:${goVersion}")
            .mountJenkinsUser()
            .inside("--volume ${WORKSPACE}:${goContainerProjectPath} -w ${goContainerProjectPath}") {
                body()
            }
}

void make(String projectPath, String goal) {
    sh "cd ${projectPath} && make ${goal}"
}

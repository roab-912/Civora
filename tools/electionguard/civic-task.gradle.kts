// Appended to the pinned egklib build file only in the .tools oracle checkout.
kotlin { sourceSets.named("jvmMain") { dependencies { implementation("org.slf4j:slf4j-simple:2.0.9") } } }
tasks.register<JavaExec>("civicVectors") {
    dependsOn("jvmJar")
    classpath(files(tasks.named<Jar>("jvmJar").flatMap { it.archiveFile }), configurations.getByName("jvmRuntimeClasspath"))
    mainClass.set("electionguard.cli.CivicVectorsKt")
    args(System.getenv("CIVIC_ORACLE_REQUEST"), System.getenv("CIVIC_ORACLE_OUTPUT"))
}

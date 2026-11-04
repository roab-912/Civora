// PUBLIC TEST SECRETS ONLY. Oracle overlay for the pinned upstream SDK.
// No upstream cryptographic implementation is patched by this harness.
package electionguard.cli

import electionguard.ballot.*
import electionguard.core.*
import electionguard.decrypt.*
import electionguard.encrypt.*
import electionguard.json2.*
import electionguard.keyceremony.*
import electionguard.input.ManifestInputValidation
import electionguard.tally.AccumulateTally
import electionguard.util.ErrorMessages
import electionguard.util.Stats
import electionguard.verifier.VerifyEncryptedBallots
import com.github.michaelbull.result.Err
import kotlinx.serialization.json.*
import java.io.File
import java.security.MessageDigest
import java.util.Base64

private val json = Json { explicitNulls = false; encodeDefaults = true; ignoreUnknownKeys = false }
private fun b64(bytes: ByteArray) = Base64.getUrlEncoder().withoutPadding().encodeToString(bytes)
private fun dec(s: String) = Base64.getUrlDecoder().decode(s)
private fun enc(p: Element) = JsonPrimitive(b64(p.byteArray()))
private fun enc(p: UInt256) = JsonPrimitive(b64(p.bytes))
private fun obj(vararg fields: Pair<String,JsonElement>) = JsonObject(fields.toMap())
private fun str(s: Any) = JsonPrimitive(s.toString())
private fun text(o: JsonObject, k: String) = o[k]!!.jsonPrimitive.content
private fun canonical(e: JsonElement): String = when(e) {
    is JsonObject -> e.keys.sorted().joinToString(",","{","}") { JsonPrimitive(it).toString()+":"+canonical(e[it]!!) }
    is JsonArray -> e.joinToString(",","[","]") { canonical(it) }
    else -> e.toString()
}
private fun hash(label: String) = MessageDigest.getInstance("SHA-256").digest(("CIVORA TEST_ONLY EG 2.0 "+label).toByteArray())
private fun proof(p: ChaumPedersenProof) = obj("challenge" to enc(p.c), "response" to enc(p.r))
private fun range(p: ChaumPedersenRangeProofKnownNonce) = JsonArray(p.proofs.map { proof(it) })
private fun cipher(c: ElGamalCiphertext) = obj("alpha" to enc(c.pad),"beta" to enc(c.data))

private class Capture(val original: DecryptingTrusteeDoerre) : DecryptingTrusteeIF by original {
    lateinit var partial: PartialDecryption
    lateinit var response: ElementModQ
    override fun decrypt(g: GroupContext, texts: List<ElementModP>): List<PartialDecryption> {
        require(texts.size == 1) { "Only the global yes_bit aggregate may be decrypted" }
        return original.decrypt(g,texts).also { partial = it.single() }
    }
    override fun challenge(g: GroupContext, requests: List<ChallengeRequest>): List<ChallengeResponse> =
        original.challenge(g,requests).also { response = it.single().response }
    fun publicTranscript() = obj("guardian_index" to str(xCoordinate()), "share" to enc(partial.Mi),
        "a" to enc(partial.a), "b" to enc(partial.b), "response" to enc(response))
}

fun main(args: Array<String>) {
    require(args.size == 2) { "request.json output.TEST_ONLY.json" }
    val request = json.parseToJsonElement(File(args[0]).readText()).jsonObject
    val g = productionGroup()
    val existing = File(args[1]).takeIf { it.exists() }?.let { json.parseToJsonElement(it.readText()).jsonObject }
    val outputs = request["contexts"]!!.jsonArray.map { input ->
        val ctx = input.jsonObject
        val poll = text(ctx,"poll_id")
        val ids = ctx["guardian_ids"]!!.jsonArray.map { it.jsonPrimitive.content }
        require(ids.size == 5 && ids.sorted() == ids && ids.distinct().size == 5)
        val trustees = ids.mapIndexed { index, id ->
            val coeffs = (0..2).map { g.binaryToElementModQsafe(hash("$poll $id coefficient $it"),2) }
            val commitments = coeffs.map { g.gPowP(it) }
            val proofs = coeffs.mapIndexed { j, s ->
                ElGamalKeypair(ElGamalSecretKey(s),ElGamalPublicKey(commitments[j]))
                    .schnorrProof(index+1,j,g.binaryToElementModQsafe(hash("$poll $id proof $j"),2))
            }
            KeyCeremonyTrustee(g,id,index+1,5,3,ElectionPolynomial(id,coeffs,commitments,proofs))
        }
        require(keyCeremonyExchange(trustees) !is Err) { "distributed encrypted share exchange failed" }
        val guardians = trustees.map { Guardian(it.id,it.xCoordinate,it.coefficientProofs()) }
        val key = ElGamalPublicKey(guardians.map { it.publicKey() }.reduce { a,b -> a*b })
        val manifest = Manifest(poll,protocolVersion,Manifest.ElectionType.general,
            "1970-01-01T00:00:00Z","9999-12-31T23:59:59Z",
            listOf(Manifest.GeopoliticalUnit("civic","civic",Manifest.ReportingUnitType.precinct,null)),
            emptyList(),listOf(Manifest.Candidate("yes_bit")),
            listOf(Manifest.ContestDescription("civic_question",0,"civic",Manifest.VoteVariationType.one_of_m,1,1,"civic_question",
                listOf(Manifest.SelectionDescription("yes_bit",0,"yes_bit")),null,null)),
            listOf(Manifest.BallotStyle("civic",listOf("civic"),emptyList(),null)),emptyList(),null)
        require(!ManifestInputValidation(manifest).validate().hasErrors())
        val manifestElement = json.encodeToJsonElement(manifest.publishJson())
        val manifestBytes = canonical(manifestElement).toByteArray(Charsets.UTF_8)
        val config = makeElectionConfig("2.0.0",g.constants,5,3,manifestBytes,false,ByteArray(0))
        val he = electionExtendedHash(config.electionBaseHash,key.key)
        val encryptor = Encryptor(g,manifest,key,he,"civic:$poll")
        val verifier = VerifyEncryptedBallots(g,manifest,key,he,config,1)
        val votes = ctx["votes_TEST_ONLY"]!!.jsonArray.map { it.jsonPrimitive.content.toInt() }
        val ballotOutputs = mutableListOf<JsonElement>()
        val ballots = votes.mapIndexed { index,vote ->
            require(vote in 0..1)
            val id = hash("$poll ballot $index").joinToString("") { "%02x".format(it) }
            val plain = PlaintextBallot(id,"civic",listOf(PlaintextBallot.Contest("civic_question",0,listOf(PlaintextBallot.Selection("yes_bit",0,vote)))))
            val errors = ErrorMessages("Civic TEST_ONLY encryption")
            val encrypted = encryptor.encrypt(plain,ByteArray(0),errors,UInt256(hash("$poll ballot nonce $index")),0L)!!
            require(!errors.hasErrors()) { errors.toString() }
            val ballot = encrypted.cast()
            require(verifier.verifyEncryptedBallot(ballot,errors,Stats())) { errors.toString() }
            val c = encrypted.contests.single()
            val s = c.selections.single()
            val compact = obj(
                "encoding" to str("eg-civic-compact-1"), "ballot_id" to str(id),
                "ballot_style_id" to str("civic"), "encrypting_device" to str("civic:$poll"),
                "timestamp" to str("0"), "code_baux" to str(""), "confirmation_code" to enc(ballot.confirmationCode),
                "election_id" to enc(he), "state" to str("CAST"),
                "contests" to JsonArray(listOf(obj("contest_id" to str("civic_question"),"sequence_order" to str("0"),
                    "votes_allowed" to str("1"),"contest_hash" to enc(c.contestHash),
                    "selections" to JsonArray(listOf(obj("selection_id" to str("yes_bit"),"sequence_order" to str("0"),
                        "encrypted_vote" to cipher(s.ciphertext),"proof" to range(s.proof)))),
                    "proof" to range(c.proof), "encrypted_contest_data" to obj("c0" to enc(c.contestData.c0),
                        "c1" to str(b64(c.contestData.c1)),"c2" to enc(c.contestData.c2),"num_bytes" to str(c.contestData.numBytes))))))
            ballotOutputs.add(obj("compact" to compact,"source_sdk_ballot" to json.encodeToJsonElement(ballot.publishJson()),
                "selection_nonce_TEST_ONLY" to enc(s.selectionNonce),"vote_TEST_ONLY" to str(vote)))
            ballot
        }
        val accumulator = AccumulateTally(g,manifest,poll,he,key)
        val errors = ErrorMessages("Civic global aggregate")
        ballots.forEach { require(accumulator.addCastBallot(it,errors)) }
        val aggregate = accumulator.build()
        val captures = listOf(0,2,4).map { i -> Capture(DecryptingTrusteeDoerre(trustees[i].id,i+1,trustees[i].guardianPublicKey(),trustees[i].computeSecretKeyShare())) }
        val decryptor = DecryptorDoerre(g,he,key,Guardians(g,guardians),captures)
        val result = with(decryptor) { aggregate.decrypt(errors) }!!
        require(!errors.hasErrors()) { errors.toString() }
        val resultSelection = result.contests.single().selections.single()
        require(resultSelection.tally == votes.sum())
        require(resultSelection.proof.verifyDecryption(he,key.key,resultSelection.encryptedVote,resultSelection.bOverM))
        val insufficientErrors = ErrorMessages("Civic negative 2/5 quorum")
        val insufficient = runCatching {
            val few = DecryptorDoerre(g,he,key,Guardians(g,guardians),captures.take(2).map { Capture(it.original.copy()) })
            with(few) { aggregate.decrypt(insufficientErrors) }
        }.getOrNull()
        require(insufficient == null || insufficientErrors.hasErrors()) { "two guardians decrypted a 3/5 election" }
        val tally = obj("encoding" to str("eg-civic-compact-1"),"id" to str(poll),"election_id" to enc(he),
            "ciphertext" to cipher(resultSelection.encryptedVote),"tally" to str(resultSelection.tally),
            "b_over_m" to enc(resultSelection.bOverM),"proof" to proof(resultSelection.proof),
            "shares" to JsonArray(captures.map { it.publicTranscript() }))
        // Reuse and validate the frozen probabilistic decryption proof. Ordinary
        // regeneration never changes a checked-in proof just to get green tests.
        val old = existing?.get("contexts")?.jsonArray?.map { it.jsonObject }?.find { text(it,"poll_id") == poll }
        val frozenTally = old?.get("tally")?.jsonObject
        if (frozenTally != null) {
            val p = frozenTally["proof"]!!.jsonObject
            val frozenProof = ChaumPedersenProof(g.binaryToElementModQ(dec(text(p,"challenge")))!!,g.binaryToElementModQ(dec(text(p,"response")))!!)
            val bom = g.binaryToElementModP(dec(text(frozenTally,"b_over_m")))!!
            require(frozenProof.verifyDecryption(he,key.key,resultSelection.encryptedVote,bom)) { "frozen global proof failed SDK verification" }
            require(text(frozenTally,"tally").toInt() == votes.sum())
            require(canonical(old["ballots"]!!) == canonical(JsonArray(ballotOutputs))) { "SDK encryption oracle diverged" }
        }
        obj("poll_id" to str(poll),"manifest" to manifestElement,
            "context" to obj("encoding" to str("eg-civic-compact-1"),"number_of_guardians" to str("5"),"quorum" to str("3"),
                "parameter_base_hash" to enc(config.parameterBaseHash),"manifest_hash" to enc(config.manifestHash),
                "election_base_hash" to enc(config.electionBaseHash),"extended_base_hash" to enc(he),"joint_public_key" to enc(key.key)),
            "guardians" to JsonArray(guardians.map { guardian -> obj("account_id" to str(guardian.guardianId),"sequence_order" to str(guardian.xCoordinate),
                "coefficient_proofs" to JsonArray(guardian.coefficientProofs.map { obj("public_key" to enc(it.publicKey),"challenge" to enc(it.challenge),"response" to enc(it.response)) })) }),
            "ballots" to JsonArray(ballotOutputs),"tally" to (frozenTally ?: tally))
    }
    val result = obj("label" to str("PUBLIC_TEST_ONLY_ELECTIONGUARD_2_0_SECRETS"),"parameters" to obj(
        "p" to str(b64(g.constants.largePrime)),"q" to str(b64(g.constants.smallPrime)),"g" to str(b64(g.constants.generator)),
        "parameter_base_hash" to enc(g.constants.hp)), "contexts" to JsonArray(outputs))
    val output = canonical(result)
    if (existing != null) require(canonical(existing) == output) { "frozen SDK oracle differs" }
    else File(args[1]).writeText(output)
    println("Civic SDK oracle: ${outputs.size} fresh 3/5 ceremonies; ballots and global tally proofs verified; 2/5 quorum rejected")
}

# 测试用例源码目录

此目录由 `python3 scripts/ci/test_inventory.py --write` 生成。它描述现有用例入口，执行结果必须看本次 Release 的证据，不能把统计数量当作通过。

| 类型 | 文件数 | 可静态识别的命名用例 |
| --- | ---: | ---: |
| go | 377 | 1679 |
| python | 62 | 275 |
| node | 178 | 181 |
| browser | 104 | 0 |

详见[Release 测试矩阵](release-validation.md)，包含基本/全量、平台、插件、人工与外部依赖的通过标准。

<details><summary>cmd/aide-local-mounts/acceptance_test.go · go · 2 个命名用例</summary>

[源码入口](../../cmd/aide-local-mounts/acceptance_test.go)

- `TestApplyRejectsStaleAndCancelled`
- `TestApplyIsolatedBrowserFixture`

环境引用：`AIDE_MOUNT_APPLY_FIXTURE`, `AIDE_MOUNT_DOCKER`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>cmd/aide-local-mounts/main_test.go · go · 8 个命名用例</summary>

[源码入口](../../cmd/aide-local-mounts/main_test.go)

- `TestCompletedMailboxRequest`
- `TestHelperSingleInstanceAndStopDuringAction`
- `TestStopTimeoutPreservesLiveOwner`
- `TestComposeChoiceAndRejectArguments`
- `TestApplyRecreatesOnlyAideAndRefreshesForwarders`
- `TestHelperStopChild`
- `TestApplyRejectsDesiredChangedDuringComposeQuery`
- `TestApplyFreezesSnapshotWhenLaterSettingsRemainPending`

环境引用：`AIDE_TEST_HELPER_CHILD`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>cmd/aide-local-mounts/process_windows_test.go · go · 3 个命名用例</summary>

[源码入口](../../cmd/aide-local-mounts/process_windows_test.go)

- `TestNativeLifecycleLockExclusionAndRelease`
- `TestNativeProcessLifetimeWithoutTasklist`
- `TestNativeComposeBoundaryKeepsLiteralDollars`

</details>

<details><summary>cmd/aide-local-mounts/stop_race_unix_test.go · go · 1 个命名用例</summary>

[源码入口](../../cmd/aide-local-mounts/stop_race_unix_test.go)

- `TestClearDeadLockOwnerUnlinksPIDAfterFirstRead`

</details>

<details><summary>internal/execsandbox/sandbox_linux_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/execsandbox/sandbox_linux_test.go)

- `TestSandboxFilesystemWrites`
- `TestSandboxSymlinkAndScriptContainment`
- `TestSandboxPrivateTemporaryDirectory`

</details>

<details><summary>internal/localmounts/config_test.go · go · 4 个命名用例</summary>

[源码入口](../../internal/localmounts/config_test.go)

- `TestPathsAndOverlap`
- `TestPrepareRightsAndDisconnectedDrive`
- `TestRejectDuplicateAndAlias`
- `TestPrepareRestoresSnapshotWhenOverrideCannotReplace`

</details>

<details><summary>internal/localmounts/relative_unicode_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/localmounts/relative_unicode_test.go)

- `TestIssue84RelativeUsesOriginalUnicodeComponents`

</details>

<details><summary>internal/messaging/consent_test.go · go · 7 个命名用例</summary>

[源码入口](../../internal/messaging/consent_test.go)

- `TestDeviceStableSeedAndKeyBinding`
- `TestContactsSeparateFromResourceConsentAndReplays`
- `TestBlockRevokesGrantsAndUnblockDoesNotRestore`
- `TestFailedPersistenceDoesNotGrantAndSnapshotsAreCopies`
- `TestInviteExpiryAndAuthenticatedReply`
- `TestConsentConcurrentBlockAndRead`
- `TestDeviceShortCodeCollisionPreservesBothFullKeys`

</details>

<details><summary>internal/messaging/lan_bridge_test.go · go · 5 个命名用例</summary>

[源码入口](../../internal/messaging/lan_bridge_test.go)

- `TestLANBridgeDockerStyleDiscoveryConsentAndOpaqueTLS`
- `TestLANBridgeStaleLeaseAndRootedMailbox`
- `TestLANBridgeKeepsAnnouncingDuringSlowApprovalRetry`
- `TestLANBridgeRejectsForeignReplyAndUnsafeTarget`
- `TestLANBridgeStopRequiresMatchingGeneration`

</details>

<details><summary>internal/messaging/lan_capture_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/messaging/lan_capture_test.go)

- `TestCaptureRelayOnDemandLeaseAndFixedDestination`

</details>

<details><summary>internal/messaging/relay_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/messaging/relay_test.go)

- `TestRealMessageThroughUntrustedOpaqueRelay`
- `TestRelayRejectsTamperingReplayAndInsecureTrust`

</details>

<details><summary>internal/messaging/service_test.go · go · 8 个命名用例</summary>

[源码入口](../../internal/messaging/service_test.go)

- `TestIssue66HistoricalTransfersDoNotExhaustActiveCapacity`
- `TestIssue66ActiveTransfersAndFailedCleanupRemainBounded`
- `TestIssue66ConcurrentRequestsCannotExceedActiveLimit`
- `TestRealNodesContactChatAndBlock`
- `TestRealFileConsentResumeTamperAndRevocation`
- `TestGroupRequiresEachSignedAcceptance`
- `TestGroupRosterRetryAfterPersistedAcceptance`
- `TestRealFileQuotaReservedBeforeContentAndReleasedOnCancel`

</details>

<details><summary>internal/messaging/store_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/messaging/store_test.go)

- `TestEncryptedAtomicStoreBindingAndRestart`

</details>

<details><summary>internal/messaging/transport_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/messaging/transport_test.go)

- `TestRealMutualTLSBoundToDeviceNotAddress`
- `TestRealTLSRejectsDelegatedDifferentLeafKey`

</details>

<details><summary>internal/messaging/unread_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/messaging/unread_test.go)

- `TestUnreadArrivalBoundarySurvivesClockSkewAndRestart`
- `TestUnreadSummaryUsesAllRetainedMessagesAndAtomicBoundaries`
- `TestUnreadLegacyMigrationAndFailedWriteRollback`

</details>

<details><summary>internal/server/apikey_vault_test.go · go · 7 个命名用例</summary>

[源码入口](../../internal/server/apikey_vault_test.go)

- `TestNoPasswordKeyEncryptedInVault`
- `TestClearKeyRemovesFromVault`
- `TestLegacyPlaintextMigration`
- `TestPasswordDoesNotRelockVault`
- `TestLegacyPasswordVaultMigratesToAccessToken`
- `TestExportNoPlaintextKey`
- `TestImportLegacyPlaintextKey`

</details>

<details><summary>internal/server/approval_intent_test.go · go · 5 个命名用例</summary>

[源码入口](../../internal/server/approval_intent_test.go)

- `TestShellApprovalExecutionIntentKeepsUnknownsExplicit`
- `TestShellApprovalIntentDescribesOnlyProvableGitRemoval`
- `TestFileApprovalExecutionIntentMatchesPendingTargets`
- `TestApprovalCommandNeedsManualUsesParsedExecutable`
- `TestApprovalGitRmTargetIsSingleSafeRelativePath`

</details>

<details><summary>internal/server/approval_policy_test.go · go · 4 个命名用例</summary>

[源码入口](../../internal/server/approval_policy_test.go)

- `TestGlobalApprovalModeManualStopsReviewAcrossSessions`
- `TestRememberedApprovalDoesNotCrossSSHRoot`
- `TestGlobalApprovalModeAssistedDoesNotMarkPausedTasksReviewing`
- `TestApprovalPolicyBroadcastSurvivesFullGlobalEventQueue`

</details>

<details><summary>internal/server/approval_receipts_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/approval_receipts_test.go)

- `TestRememberedApprovalReceiptAndOutcome`

</details>

<details><summary>internal/server/approval_reviewer_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/approval_reviewer_test.go)

- `TestApprovalReviewerBoundedRecovery`
- `TestApprovalReviewerErrorsNeverApprove`
- `TestAssistedBasicCommandRecoversAndExecutesOnce`

</details>

<details><summary>internal/server/approval_rules_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/approval_rules_test.go)

- `TestApprovalRememberExpiryFromAnswer`
- `TestApprovalRuleManagement`
- `TestConcurrentApprovalExpiryAndRevocationUseRevisionGuard`

</details>

<details><summary>internal/server/archive_test.go · go · 9 个命名用例</summary>

[源码入口](../../internal/server/archive_test.go)

- `TestWorkspaceDownloadAndArchive`
- `TestReferenceDownloadAndSafeExtraction`
- `TestReadArchiveFilesRejectsPortablePathTraversal`
- `TestReadArchiveFilesDeduplicatesNormalizedPaths`
- `TestIssue69ArchiveRejectsFileParentConflictsBeforeExtraction`
- `TestIssue69FailedWorkspaceExtractionCleansStage`
- `TestIssue69ArchivePublicationPreservesExistingDestination`
- `TestWorkspaceArchiveFormatsRoundTrip`
- `TestTarExtractionRejectsTraversalAndLinks`

</details>

<details><summary>internal/server/assistant_agent_test.go · go · 6 个命名用例</summary>

[源码入口](../../internal/server/assistant_agent_test.go)

- `TestAssistantAgenticChatNoDispatch`
- `TestAssistantAgenticPluginResultReturnedToModel`
- `TestAssistantAgenticDispatch`
- `TestAssistantAgenticSilent`
- `TestAssistantAgenticSelfIdentity`
- `TestAssistantRecentConversationRestoresShortTermContext`

</details>

<details><summary>internal/server/assistant_call_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/assistant_call_test.go)

- `TestAssistantCallUsesSpokenGuidanceAndBoundedReply`
- `TestAssistantCallNeverFallsBackToWorkDispatch`

</details>

<details><summary>internal/server/assistant_compaction_gate_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/assistant_compaction_gate_test.go)

- `TestIssue113LockedAssistantCompactionCannotReadOrSendHistory`
- `TestIssue113AuthorizedAndOrdinarySessionCompactionRemainAvailable`
- `TestIssue113RelockBeforeCompactionCommitPreservesHistory`

</details>

<details><summary>internal/server/assistant_conversation_style_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/assistant_conversation_style_test.go)

- `TestAssistantNaturalDispatchStyle`
- `TestAssistantConciseResultKeepsFacts`
- `TestMixSoulUnverifiedDispatchRetriesWithTool`

</details>

<details><summary>internal/server/assistant_delegation_test.go · go · 4 个命名用例</summary>

[源码入口](../../internal/server/assistant_delegation_test.go)

- `TestAssistantPushActuallyStartsAndLabelsTask`
- `TestAssistantApprovalSettingScopePreservationAndRevocation`
- `TestAssistantPushNeverAnswersPendingApproval`
- `TestAssistantReviewerAppliesNewFileUsingOriginalAuthority`

</details>

<details><summary>internal/server/assistant_delivery_browser_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/assistant_delivery_browser_test.go)

- `TestAssistantDeliveryBrowserFixture`

环境引用：`AIDE_ASSISTANT_BROWSER_ADDRESS`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/assistant_delivery_loop_test.go · go · 12 个命名用例</summary>

[源码入口](../../internal/server/assistant_delivery_loop_test.go)

- `TestAssistantDeliveryEvidenceGate`
- `TestAssistantDeliveryPromiseReturnsIncomplete`
- `TestAssistantMediaIntentContinuity`
- `TestAssistantMediaFailureClassification`
- `TestAssistantParallelProgressBudgetAndSimilarity`
- `TestAssistantHistoryRetrievesOldMatchesAndPages`
- `TestAssistantMemoryNotesSourceAndRevocation`
- `TestAssistantLedgerIncludesBlockersDeliveryAndSchedules`
- `TestAssistantScheduleSurfacesPausedDelivery`
- `TestAssistantProjectMemoryRetrievesBeyondPrefix`
- `TestAssistantNativeStopDoesNotQueueOrResubmit`
- `TestAssistantMediaContinuationPinsModelWithoutAuto`

</details>

<details><summary>internal/server/assistant_followup_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/assistant_followup_test.go)

- `TestAssistantClearFollowupsIsReversibleAndPreservesTasks`
- `TestAssistantClearFollowupsTargetsCurrentExplicitReferences`

</details>

<details><summary>internal/server/assistant_history_test.go · go · 9 个命名用例</summary>

[源码入口](../../internal/server/assistant_history_test.go)

- `TestContextToolsForAssistantOnly`
- `TestAssistantMessageRecordsAndDispatches`
- `TestAssistantMessageRejectsNonAssistant`
- `TestVoiceFilterRecordsToAssistantSession`
- `TestDispatchToAideLockedCreatesSession`
- `TestStartDispatchedAssistantTaskStartsRun`
- `TestAssistantMessageAgenticFailureDispatchesTypedRequest`
- `TestAssistantMessageGateLockedUnlockedClear`
- `TestVoiceFilterGateLocked`

</details>

<details><summary>internal/server/assistant_initiative_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/assistant_initiative_test.go)

- `TestAssistantInitiativeGroundedAndOneShot`
- `TestAssistantInitiativeUnsafeSpeech`

</details>

<details><summary>internal/server/assistant_mixsoul_test.go · go · 9 个命名用例</summary>

[源码入口](../../internal/server/assistant_mixsoul_test.go)

- `TestMixSoulBuiltInEncryptedLifecycle`
- `TestMixSoulPluginCannotSelectEngine`
- `TestMixSoulRecallAfterRestartWithoutConversationHistory`
- `TestMixSoulNativeTimeRefreshDoesNotGenerateResponses`
- `TestMixSoulNativeLearningPersistsBoundedImportance`
- `TestMixSoulPrivateAcceptanceMetadata`
- `TestMixSoulEmotionHostDialogue`
- `TestMixSoulOwnerManagementGatesAndNoModelCalls`
- `TestMixSoulLocalEmbeddingOperatorConfigAndGates`

</details>

<details><summary>internal/server/assistant_native_memory_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/assistant_native_memory_test.go)

- `TestMixSoulNativeLateASRHangupPersistsEvidence`
- `TestMixSoulNativeWorkCannotRecallFromVoiceHistory`

</details>

<details><summary>internal/server/assistant_native_quotation_socket_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/assistant_native_quotation_socket_test.go)

- `TestMixSoulNativeQuotationLateASRRejectsShortenedAudioBeforePlayback`

</details>

<details><summary>internal/server/assistant_native_quotation_test.go · go · 5 个命名用例</summary>

[源码入口](../../internal/server/assistant_native_quotation_test.go)

- `TestNativeQuotationRejectsShorteningAndPreservesIndependentAffectReview`
- `TestNativeQuotationIsBoundedAndCannotPersistUnreviewedSpeech`
- `TestNativeQuotationRetryPrioritizesLiteralDataOverBrevity`
- `TestNativeQuotationDoesNotEraseMeaningfulNumberOrIdentifierSeparators`
- `TestNativeQuotationAllowsOnlyLinkedExplicitHistoricalAffect`

</details>

<details><summary>internal/server/assistant_native_timing_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/assistant_native_timing_test.go)

- `TestNativeClockRefreshWaitsForMicrophoneAndPlaybackIdle`
- `TestNativeVADModeAndASRUseSupportedProviderContract`

</details>

<details><summary>internal/server/assistant_native_turns_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/assistant_native_turns_test.go)

- `TestNativeReplyLateASRAndNextInput`
- `TestNativeCanceledAndAmbiguousRepliesFailClosed`

</details>

<details><summary>internal/server/assistant_realtime_live_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/assistant_realtime_live_test.go)

- `TestAssistantRealtimeLiveSyntheticAudio`

环境引用：`AIDE_NATIVE_LIVE_DATA`, `AIDE_NATIVE_LIVE_FIXTURES`, `AIDE_NATIVE_LIVE_PCM_OUTPUT`, `AIDE_NATIVE_LIVE_SYNTHETIC_TEXT`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/assistant_realtime_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/assistant_realtime_test.go)

- `TestAssistantRealtimeEndpointAndAuth`
- `TestAssistantRealtimeAudioRelayAndLock`

</details>

<details><summary>internal/server/assistant_result_trigger_test.go · go · 4 个命名用例</summary>

[源码入口](../../internal/server/assistant_result_trigger_test.go)

- `TestAssistantResultTriggerPersistentAndScoped`
- `TestAssistantResultTriggerLockAndDelivery`
- `TestAssistantResultTriggerRealHTTP`
- `TestAssistantResultTriggerFullRestart`

</details>

<details><summary>internal/server/assistant_speech_review_test.go · go · 6 个命名用例</summary>

[源码入口](../../internal/server/assistant_speech_review_test.go)

- `TestNativeSpeechReviewHoldsSpeculativeAudioAndBoundsBuffer`
- `TestNativeSpeechAssertionsKeepQuotedEvidenceDistinct`
- `TestNativeKnownResponseConflictDoesNotHideUnknownProviderErrors`
- `TestNativeProviderDiagnosticIsPrivateBoundedAndRedacted`
- `TestNativeCalibratedSpeechIsBoundedAndQuotesCurrentUtterance`
- `TestNativeEmotionEvidenceDenialDoesNotBecomeAssertion`

</details>

<details><summary>internal/server/assistant_watch_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/assistant_watch_test.go)

- `TestAssistantWatchReadOnlyLifecycle`
- `TestAssistantWorkLedgerPrioritizesBlockersAndVisibleResults`
- `TestAssistantVoiceUsesAgentAndNeverDraftFallback`

</details>

<details><summary>internal/server/assisted_approval_files_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/assisted_approval_files_test.go)

- `TestAssistedApprovalLocalFiles`
- `TestReviewedFileResult`

</details>

<details><summary>internal/server/attachment_budget_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/attachment_budget_test.go)

- `TestAttachmentBudgetBeyondLegacy195KiB`
- `TestAttachmentBudgetMultipleFilesAndCapacity`
- `TestAttachmentBudgetProcessingGuardDoesNotReturnPartialInput`

</details>

<details><summary>internal/server/attachment_context_budget_test.go · go · 4 个命名用例</summary>

[源码入口](../../internal/server/attachment_context_budget_test.go)

- `TestAttachmentContextResourceBoundary`
- `TestAttachmentContextUsesModelBudget`
- `TestAttachmentContextUnreadableDoesNotSilentlyOmit`
- `TestAttachmentContextLargeDocumentsKeepTruncationNotice`

</details>

<details><summary>internal/server/attachments_size_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/attachments_size_test.go)

- `TestReadLocalImageHasNoFiveMiBLimit`

</details>

<details><summary>internal/server/attachments_test.go · go · 10 个命名用例</summary>

[源码入口](../../internal/server/attachments_test.go)

- `TestTruncateExtract`
- `TestModelSupportsVision`
- `TestOutgoingMessagesMultimodal`
- `TestAttachmentContextDocx`
- `TestAttachmentContextXlsx`
- `TestAttachmentContextPdf`
- `TestAttachmentContextUnsupportedBinary`
- `TestVisionGateLockedDoesNotFilterImagesByModel`
- `TestReadLocalImageDataURL`
- `TestAttachmentContextDirectory`

</details>

<details><summary>internal/server/auth_passkey_origin_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/auth_passkey_origin_test.go)

- `TestMasterPasskeyDynamicOriginAndReplay`
- `TestMasterAuthTokenFailureIsDistinct`
- `TestPasskeyCounterCommitRechecksCurrentState`

</details>

<details><summary>internal/server/auth_verify_test.go · go · 4 个命名用例</summary>

[源码入口](../../internal/server/auth_verify_test.go)

- `TestMasterAuthUnlocksRequestedAssistantSession`
- `TestMasterAuthCannotUnlockNonAssistantSession`
- `TestLockStatePasswordUnlockCommitsBeforeSuccess`
- `TestLockStateLocalPasswordReceiptPreservesGlobalLock`

</details>

<details><summary>internal/server/auto_model_router_empty_key_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/auto_model_router_empty_key_test.go)

- `TestAutoModelRouterEmptyProviderKey`

</details>

<details><summary>internal/server/auto_model_router_test.go · go · 8 个命名用例</summary>

[源码入口](../../internal/server/auto_model_router_test.go)

- `TestAutoModelRouterOfflineGuards`
- `TestAutoModelRouterParallelProviders`
- `TestAutoModelRouterSequentialAndSelection`
- `TestAutoModelRouterCancellation`
- `TestAutoModelRouterInvalidCatalogs`
- `TestAutoModelRouterSequentialHTTP`
- `TestAutoModelRouterWireParameters`
- `TestAutoModelRouterDisableBetweenRequests`

</details>

<details><summary>internal/server/autonomous_execution_test.go · go · 5 个命名用例</summary>

[源码入口](../../internal/server/autonomous_execution_test.go)

- `TestAutonomousExecutionPolicy`
- `TestClarificationOptionsRemainAnswerable`
- `TestClarificationCardCorrection`
- `TestAutonomousResearchContinuation`
- `TestAutonomousExplicitPDFReadRequiresAttempt`

</details>

<details><summary>internal/server/avatar_feedback_test.go · go · 9 个命名用例</summary>

[源码入口](../../internal/server/avatar_feedback_test.go)

- `TestAvatarCueValidationAndBudget`
- `TestAvatarToolLoopSingleCompletionAndDisabled`
- `TestAvatarAssistantSingleCompletion`
- `TestAvatarAssistantHTTPOptIn`
- `TestAvatarContextPreviewIncludesToolCost`
- `TestAvatarFinishStreamPreservesTerminalStatus`
- `TestAvatarBudgetSpansWorkflowSteps`
- `TestAvatarRetryResumeHonorCurrentOptOut`
- `TestAvatarLateSSEReplaysCueAndStatus`

</details>

<details><summary>internal/server/benchmark_phase_timing_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/benchmark_phase_timing_test.go)

- `TestBenchmarkPhaseRecorderNoPayloadAndNoDuplicateIntervals`
- `TestLargeEngineeringPhaseTimingLiveLuna`

环境引用：`AIDE_STAGE_CASES`, `AIDE_STAGE_CONDITION`, `AIDE_STAGE_LIVE_DATA`, `AIDE_STAGE_TRACE`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/browser_control_test.go · go · 5 个命名用例</summary>

[源码入口](../../internal/server/browser_control_test.go)

- `TestBrowserToolConfirmationBoundary`
- `TestBrowserActionConfirmationContainsTarget`
- `TestBrowserFillConfirmationDoesNotStoreTypedContent`
- `TestComputerToolConfirmationBoundary`
- `TestComputerConfirmationDoesNotStoreTypedContent`

</details>

<details><summary>internal/server/button_feedback_browser_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/button_feedback_browser_test.go)

- `TestButtonFeedbackBrowser`

环境引用：`AIDE_BUTTON_FEEDBACK_BROWSER`, `AIDE_EVIDENCE_DIR`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/cancelled_output_context_test.go · go · 8 个命名用例</summary>

[源码入口](../../internal/server/cancelled_output_context_test.go)

- `TestCancelledOutputLegacyContext`
- `TestCancelledOutputProviderKeepsPartialWhileWaiting`
- `TestCancelledOutputBoundaryProjection`
- `TestCancelledOutputLegacyOrderAndFingerprint`
- `TestCancelledOutputHTTPFollowupAndPauseResume`
- `TestCancelledOutputLegacyResumedLineage`
- `TestCancelledOutputSuccessfulStagesReachHistory`
- `TestCancelledOutputMaterializeBeforeCompaction`

</details>

<details><summary>internal/server/capabilities_plugin_live_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/capabilities_plugin_live_test.go)

- `TestCapabilityPluginLiveLifecycle`

</details>

<details><summary>internal/server/capabilities_test.go · go · 7 个命名用例</summary>

[源码入口](../../internal/server/capabilities_test.go)

- `TestCapabilityWriteProbeCreatesAndCleansTemporaryFile`
- `TestCapabilityWriteProbeHonorsConfiguredReadOnly`
- `TestCapabilityLocalChecksAndStaleness`
- `TestCapabilityPluginAndSourceBoundaries`
- `TestCapabilityModelMetadataAndConcurrentChange`
- `TestCapabilityMCPDiscoveryDoesNotCallTools`
- `TestCapabilityParserDependencyObservations`

</details>

<details><summary>internal/server/command_input_test.go · go · 4 个命名用例</summary>

[源码入口](../../internal/server/command_input_test.go)

- `TestCommandInputForwardsExactBytesToActivePTY`
- `TestCommandInputForwardsTabWithoutSubmittingLine`
- `TestCommandInputRejectsClosedSession`
- `TestSSHInteractiveSessionStartsLoginShellAndFeedsFirstCommand`

</details>

<details><summary>internal/server/command_panel_browser_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/command_panel_browser_test.go)

- `TestCommandPanelBrowser`
- `TestCommandPanelRestrictedSSHRejectsBeforeConnection`

环境引用：`AIDE_COMMAND_PANEL_BROWSER`, `AIDE_EVIDENCE_DIR`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/comment_reply_bounds_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/comment_reply_bounds_test.go)

- `TestIssue83CommentReplyFieldsBoundBeforePersistence`
- `TestIssue83CommentReplyUnicodeBoundary`

</details>

<details><summary>internal/server/comments_test.go · go · 8 个命名用例</summary>

[源码入口](../../internal/server/comments_test.go)

- `TestCommentsCRUD`
- `TestCommentsStale`
- `TestCommentsPathSafety`
- `TestCommentsFilePerms`
- `TestDocxStructureTool`
- `TestDocxCommentRoundTrip`
- `TestDocxToolPathSafety`
- `TestLegacyDocRejected`

</details>

<details><summary>internal/server/completion_delivery_test.go · go · 6 个命名用例</summary>

[源码入口](../../internal/server/completion_delivery_test.go)

- `TestAgentReviewUnfinishedWritingPlan`
- `TestCompletionReviewPreservesOriginalDelivery`
- `TestUnfinishedWritingPlanContinuesThroughReview`
- `TestDeliveryPromiseWithoutPlan`
- `TestDeliveryPromiseLoopBounded`
- `TestDeliveryPromiseDoesNotReplacePlanningStep`

</details>

<details><summary>internal/server/composer_approval_gate_test.go · go · 6 个命名用例</summary>

[源码入口](../../internal/server/composer_approval_gate_test.go)

- `TestIssue108ComposerCannotResolveApprovalQuestions`
- `TestIssue108ApprovalSaveFailureNeverDeliversAnswer`
- `TestIssue108MalformedQuestionCannotBeAnswered`
- `TestIssue108ApprovalAnswerRequiresCurrentQuestionAndRound`
- `TestIssue108BoundApprovalPersistsHumanReceiptBeforeDelivery`
- `TestIssue108ComposerStillAnswersOrdinaryClarification`

</details>

<details><summary>internal/server/config_backup_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/config_backup_test.go)

- `TestConfigBackupExportImport`
- `TestConfigBackupImportSecretsSwitch`

</details>

<details><summary>internal/server/config_import_test.go · go · 10 个命名用例</summary>

[源码入口](../../internal/server/config_import_test.go)

- `TestImportMissingFieldsDefaults`
- `TestImportExplicitValuesPreserved`
- `TestImportExplicitZeroValues`
- `TestImportInvalidContextWindow`
- `TestImportInvalidMaxToolRounds`
- `TestImportSensitiveFieldsNotImported`
- `TestImportHigherVersionRejected`
- `TestImportNoRestartNeeded`
- `TestMigrateHookV0toV1`
- `TestDesensitizationPreserved`

</details>

<details><summary>internal/server/context_breakdown_test.go · go · 5 个命名用例</summary>

[源码入口](../../internal/server/context_breakdown_test.go)

- `TestContextBreakdownSeparatesRequestPartsAndSums`
- `TestContextPreviewBaselineAllowsEmptyPromptWithoutRequestBody`
- `TestContextPreviewAccumulatesHistoryAndReflectsCompaction`
- `TestSessionHistoryPageIncludesLifetimeUsageAcrossAllRuns`
- `TestContextParallelTransientAnchorDoesNotInflateBaseline`

</details>

<details><summary>internal/server/context_recovery_live_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/context_recovery_live_test.go)

- `TestContextRecoveryLiveModels`

环境引用：`AIDE_CONTEXT_LIVE_DATA`, `AIDE_CONTEXT_LIVE_MODELS`, `AIDE_CONTEXT_LIVE_PARALLELISM`, `AIDE_CONTEXT_LIVE_PROOF`, `AIDE_CONTEXT_LIVE_REASONING_EFFORT`, `AIDE_CONTEXT_LIVE_TASKS`, `AIDE_CONTEXT_LIVE_TIMEOUT_SECONDS`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/context_recovery_test.go · go · 18 个命名用例</summary>

[源码入口](../../internal/server/context_recovery_test.go)

- `TestContextRecoveryRetainsUncompactedHistory`
- `TestCompactionTransportRetryIsBoundedAndCancelable`
- `TestContextRecoveryCompactionKeepsToolExchange`
- `TestContextRecoverySummarySeesMiddleContent`
- `TestContextRecoveryStreamEOFIsNotSuccess`
- `TestContextRecoveryActiveBudgetPreservesScopeAndProtocol`
- `TestContextRecoveryActiveFailurePreservesInput`
- `TestContextRecoveryOversizedLastMessageCanBeSummarized`
- `TestContextRecoveryPrematureStreamKeepsResumableCheckpoint`
- `TestContextRecoveryCompactionInvalidatesOldUsageAnchor`
- `TestContextRecoverySummaryNormalizesTextShapesOnly`
- `TestContextRecoverySummaryExtractsWrappedJSON`
- `TestContextRecoverySummaryRetriesAndSurvivesRepeatedRuns`
- `TestContextRecoveryPreflightCompactsMultipleHistoryBatches`
- `TestContextRecoverySummaryFingerprintInvalidatesOldAnchor`
- `TestContextRecoveryActiveCompactionFailureRetainsPartial`
- `TestContextRecoveryTruncatedToolCallNeverDispatches`
- `TestLiveParallelNumberedHeadingCoverageIgnoresDecimals`

</details>

<details><summary>internal/server/context_sources_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/context_sources_test.go)

- `TestContextSourcePersistentAcrossWorkspaceSwitch`
- `TestListSourcesToolReturnsBuiltinPlusCustom`
- `TestDefaultWorkspaceContextNoRegression`

</details>

<details><summary>internal/server/control_workflow_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/control_workflow_test.go)

- `TestControlWorkflowConfirmationAndRevocation`
- `TestControlPluginCanceledContext`
- `TestControlStatusResultsReachModel`

</details>

<details><summary>internal/server/conversation_parallel_test.go · go · 21 个命名用例</summary>

[源码入口](../../internal/server/conversation_parallel_test.go)

- `TestParallelWorkerTokenBudgetPreservesDetailWithinContext`
- `TestConversationParallelAdaptiveEvidence`
- `TestConversationParallelAnalysisContextIsolation`
- `TestConversationParallelEvidenceIgnoresBookkeepingAndReplay`
- `TestConversationParallelAdaptiveToolLoop`
- `TestConversationParallelAdaptiveWavesAndLifecycle`
- `TestConversationParallelUsesMainRoundBudget`
- `TestConversationParallelPlan`
- `TestExplicitParallelTasks`
- `TestParallelExplicitNoToolDraftFastAssembly`
- `TestNumberedParallelAssemblyPreservesDecimalFacts`
- `TestExplicitParallelRetryReusesCompletedAssignments`
- `TestParallelDraftQualityRejectsMissingRequestedContent`
- `TestConversationParallelFanout`
- `TestConversationParallelFitsSharedWorkerContextOnce`
- `TestConversationParallelFallback`
- `TestConversationParallelCancel`
- `TestConversationParallelValidation`
- `TestConversationParallelEndToEnd`
- `TestConversationParallelBudgetAndResume`
- `TestConversationParallelROS2Document`

</details>

<details><summary>internal/server/crash_diagnostics_test.go · go · 6 个命名用例</summary>

[源码入口](../../internal/server/crash_diagnostics_test.go)

- `TestCrashPanicResponseAndIsolation`
- `TestCrashWriterCapabilitiesAndCommittedStream`
- `TestCrashHijackedConnectionCleanup`
- `TestCrashConcurrentRequests`
- `TestCrashHealthInstanceIdentity`
- `TestCrashHTTPWireRecovery`

</details>

<details><summary>internal/server/current_time_plugin_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/current_time_plugin_test.go)

- `TestCurrentTimePluginAvailableToAideAndXiaomi`

</details>

<details><summary>internal/server/custody_keys_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/custody_keys_test.go)

- `TestCustodyKeysStayEncryptedAndMetadataOnly`
- `TestCustodyKeysRequireUnlockedVaultAndRespectNamespace`

</details>

<details><summary>internal/server/daemon_event_bounds_test.go · go · 4 个命名用例</summary>

[源码入口](../../internal/server/daemon_event_bounds_test.go)

- `TestIssue86DaemonEventsBoundEncodedPayloadAndHistory`
- `TestIssue86DaemonEventCountAndOrdinaryPayload`
- `TestIssue86ManagerGlobalBudgetEvictsOldestAndBoundsAPI`
- `TestIssue86ConcurrentProducersAndReaders`

</details>

<details><summary>internal/server/debug_redaction_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/debug_redaction_test.go)

- `TestIssue73DebugSnapshotsExcludeContent`
- `TestIssue73DebugEventsExcludeContent`

</details>

<details><summary>internal/server/debug_test.go · go · 4 个命名用例</summary>

[源码入口](../../internal/server/debug_test.go)

- `TestDebugUsesVaultBackedModelCredential`
- `TestDebugPingUsesVaultBackedModelCredential`
- `TestDebugAuditNoiseReduction`
- `TestDebugAuditExport`

</details>

<details><summary>internal/server/directory_picker_test.go · go · 4 个命名用例</summary>

[源码入口](../../internal/server/directory_picker_test.go)

- `TestDirectoryPickerCreatesAndRenamesWorkspaceFolder`
- `TestDirectoryCreationUsesSelectedWritableReference`
- `TestFileManagerPropertiesAndDelete`
- `TestDirectoryPickerRejectsUnsafeNamesAndRemoteControlCharacters`

</details>

<details><summary>internal/server/document_proposals_test.go · go · 8 个命名用例</summary>

[源码入口](../../internal/server/document_proposals_test.go)

- `TestDocumentProposalBindingAndMixedPaths`
- `TestDocumentProposalBindingChangeRejectsOldApproval`
- `TestDocumentProposalExistingAttachmentAndVersion`
- `TestDocumentProposalRejectsUnavailableAndEscapedBinding`
- `TestDocumentProposalIndependentReviewUsesBoundRoot`
- `TestSourceProposalVisibilityAndWriteLocks`
- `TestSourceProposalRejectsLockedCreation`
- `TestSourceProposalIndependentSFTP`

</details>

<details><summary>internal/server/download_version_test.go · go · 6 个命名用例</summary>

[源码入口](../../internal/server/download_version_test.go)

- `TestIssue78MutableFileDoesNotOfferStrongRangeValidator`
- `TestIssue78RawDownloadSnapshotPersistsAndStaysImmutable`
- `TestIssue78RebuiltSnapshotChangesValidator`
- `TestIssue78SnapshotRejectsInvalidSelection`
- `TestIssue78SnapshotExceedsPreviewByteLimit`
- `TestIssue78SFTPFileSnapshot`

</details>

<details><summary>internal/server/empty_completion_test.go · go · 6 个命名用例</summary>

[源码入口](../../internal/server/empty_completion_test.go)

- `TestCompleteStreamEmptyIsSentinel`
- `TestCompleteStreamEmptyCarriesFinishReason`
- `TestToolLoopEmptyFallbackRecovers`
- `TestToolLoopEmptyFallbackExhausted`
- `TestToolLoopUpstreamErrorPreservesChain`
- `TestAnalyzeShellFailureDwgHint`

</details>

<details><summary>internal/server/environment_guide_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/environment_guide_test.go)

- `TestEnvironmentGuide`

</details>

<details><summary>internal/server/execution_environments_live_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/execution_environments_live_test.go)

- `TestExecutionEnvironmentsLiveLuna`

环境引用：`AIDE_ENVIRONMENT_LIVE_DATA`, `AIDE_ENVIRONMENT_LIVE_PROOF`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/execution_environments_test.go · go · 10 个命名用例</summary>

[源码入口](../../internal/server/execution_environments_test.go)

- `TestExecutionEnvironmentLazyCapacityAndExchange`
- `TestExecutionEnvironmentScratchAndMergeConflicts`
- `TestExecutionEnvironmentSnapshotDoesNotFollowLinks`
- `TestExecutionEnvironmentWorkerToolLoopAndRelease`
- `TestExecutionEnvironmentQueueCancellationAndDependency`
- `TestExecutionEnvironmentPrivatePluginAndSandbox`
- `TestExecutionEnvironmentToolDisclosureAndRecovery`
- `TestExecutionEnvironmentApprovalQueueUsesOriginalCWD`
- `TestExecutionEnvironmentSingleCancelDoesNotCancelPool`
- `TestExecutionEnvironmentDaemonInstancesArePrivate`

</details>

<details><summary>internal/server/execution_queue_drain_test.go · go · 4 个命名用例</summary>

[源码入口](../../internal/server/execution_queue_drain_test.go)

- `TestExecutionQueueDrainAllTenSlotsWithoutManager`
- `TestExecutionQueueDrainCancelsAndJoinsBeforeResumingMain`
- `TestExecutionQueueDrainValidationDependenciesAndConflict`
- `TestExecutionQueueDrainRandomReadyRespectsDependencies`

</details>

<details><summary>internal/server/execution_result_receipt_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/execution_result_receipt_test.go)

- `TestExecutionCollectCompactRetainsExplicitDetail`
- `TestExecutionCollectSelectedPathsPreservesConflictAndApproval`

</details>

<details><summary>internal/server/execution_scheduler_perf_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/execution_scheduler_perf_test.go)

- `TestExecutionSchedulerInstructionUsesMeasuredGranularityGuidance`
- `TestExecutionSchedulerAsyncDefaultAndExplicitBlockingAlternative`
- `TestExecutionSchedulerCurrentPolicyOnResumeWithoutPreviewDuplication`

</details>

<details><summary>internal/server/execution_scheduler_receipt_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/execution_scheduler_receipt_test.go)

- `TestExecutionSchedulerReceiptRebuildsAfterCompaction`
- `TestExecutionSchedulerReceiptLeavesOrdinaryProjectUnchanged`
- `TestExecutionSchedulerReceiptBoundsPromptWithoutLimitingWork`

</details>

<details><summary>internal/server/execution_work_queue_test.go · go · 8 个命名用例</summary>

[源码入口](../../internal/server/execution_work_queue_test.go)

- `TestExecutionWorkQueueFiftyJobsFillVacanciesBeforeCollection`
- `TestExecutionWorkQueueRejectsInvalidBatchAtomically`
- `TestExecutionWorkQueueDependencyFailureDoesNotBlockIndependentWork`
- `TestExecutionWorkQueueForwardDependencyAndReadyWaitCancellation`
- `TestExecutionWorkQueueReadyMergeKeepsOtherResultsOnConflict`
- `TestExecutionWorkQueueToolDispatchAndWorkerBoundary`
- `TestExecutionWorkQueueCheckpointFailureStartsNothing`
- `TestExecutionWorkQueueConcurrentSubmitAndClose`

</details>

<details><summary>internal/server/factory_reset_failure_test.go · go · 5 个命名用例</summary>

[源码入口](../../internal/server/factory_reset_failure_test.go)

- `TestIssue120ResetPersistenceFailuresNeverReportSuccess`
- `TestIssue120PartialResetReportsCompletedScopeAndBackup`
- `TestIssue120BackupFailureDoesNotStartReset`
- `TestIssue120AuditFailureAndUniqueRecoveryBackups`
- `TestIssue120UnreadableBucketIsNotAnEmptySuccessfulReset`

</details>

<details><summary>internal/server/factory_reset_test.go · go · 10 个命名用例</summary>

[源码入口](../../internal/server/factory_reset_test.go)

- `TestFactoryReset_SettingsOnly`
- `TestFactoryReset_SessionsAndMemory`
- `TestFactoryReset_Credentials`
- `TestFactoryReset_All`
- `TestFactoryReset_BackupGenerated`
- `TestFactoryReset_UserFilesPreserved`
- `TestFactoryReset_UnauthorizedRejected`
- `TestFactoryReset_AuditWritten`
- `TestFactoryReset_PasswordRequired`
- `TestFactoryReset_Preview`

</details>

<details><summary>internal/server/file_git_browser_fixture_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/file_git_browser_fixture_test.go)

- `TestFileGitHistoryBrowserFixture`

环境引用：`AIDE_GIT_HISTORY_BROWSER_FIXTURE`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/file_git_history_ssh_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/file_git_history_ssh_test.go)

- `TestFileGitHistorySSHCompare`
- `TestFileGitHistorySSHBoundaries`
- `TestSSHLiveGitHistory`

环境引用：`AIDE_SSH_FIXTURE_A`, `AIDE_SSH_TEST_ISOLATED`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/file_git_history_test.go · go · 4 个命名用例</summary>

[源码入口](../../internal/server/file_git_history_test.go)

- `TestFileGitHistoryCompare`
- `TestFileGitHistoryBoundaries`
- `TestFileGitHistoryWorktree`
- `TestFileGitHistorySourceAndUnborn`

</details>

<details><summary>internal/server/file_transfer_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/file_transfer_test.go)

- `TestFileTransferMultipleAndDirectories`
- `TestFileTransferReferencesAndBoundaries`
- `TestFileTransferWritableReference`

</details>

<details><summary>internal/server/file_unix_metadata_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/file_unix_metadata_test.go)

- `TestFileUnixPermissions`
- `TestFileUnixLocalListing`
- `TestFileUnixRemoteReported`

</details>

<details><summary>internal/server/file_upload_batch_test.go · go · 8 个命名用例</summary>

[源码入口](../../internal/server/file_upload_batch_test.go)

- `TestBatchUploadNestedLayout`
- `TestBatchUploadTraversalRejectedPerFile`
- `TestCleanBatchRelStrict`
- `TestBatchUploadConflictDoesNotAbort`
- `TestBatchUploadCountCap`
- `TestBatchUploadTotalSizeCap`
- `TestBatchUploadSourceRules`
- `TestBatchUploadEmpty`

</details>

<details><summary>internal/server/file_upload_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/file_upload_test.go)

- `TestFileUploadAndSafeHiddenListing`
- `TestFileSearchSupportsRecursiveFuzzyAndExactMatching`

</details>

<details><summary>internal/server/files_limit_test.go · go · 8 个命名用例</summary>

[源码入口](../../internal/server/files_limit_test.go)

- `TestTextFileAboveOld256KiBNowOK`
- `TestTextFileAboveFormerCapAccepted`
- `TestValidateTextContentUnbounded`
- `TestReadTextLinesPaging`
- `TestReadTextLinesRejectsBinaryAnywhere`
- `TestHTTPFileRangeWindow`
- `TestTextRejectBinaryAndUnsafePath`
- `TestJSONUTF16BOMOpensAsText`

</details>

<details><summary>internal/server/files_search_truncation_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/files_search_truncation_test.go)

- `TestFileSearchTruncationHeaderOnResultCap`
- `TestFileSearchNoHeaderWhenUnderCap`

</details>

<details><summary>internal/server/files_window_test.go · go · 4 个命名用例</summary>

[源码入口](../../internal/server/files_window_test.go)

- `TestStreamTextWindowByteSegmentsLongLine`
- `TestWorkspaceByteCoverageRequiresCompleteSingleVersion`
- `TestStreamTextWindowLineRangeAndOversizedLine`
- `TestStreamTextWindowNormalizesUTF16JSON`

</details>

<details><summary>internal/server/harness_integration_test.go · go · 13 个命名用例</summary>

[源码入口](../../internal/server/harness_integration_test.go)

- `TestHarnessConfigLayersAndStrictValidation`
- `TestHarnessCheckpointRecoveryAndJournalOwnership`
- `TestHarnessCheckpointFailurePreventsDispatch`
- `TestHarnessSkillsToolsAndHookExecution`
- `TestHarnessApprovalUsesRootAuthorization`
- `TestHarnessPluginServiceAssembly`
- `TestHarnessManagedWorktreeCreateArchive`
- `TestHarnessTaskSnapshotSurvivesRestart`
- `TestHarnessChildUsesConfiguredModelAndNarrowTools`
- `TestHarnessPluginContractsRejectInvalidAssembly`
- `TestHarnessManualApprovalSaveFailureDoesNotRelease`
- `TestHarnessIndependentReviewerReleaseAndSandbox`
- `TestHarnessReadOnlyCommandOptions`

</details>

<details><summary>internal/server/incidents_test.go · go · 8 个命名用例</summary>

[源码入口](../../internal/server/incidents_test.go)

- `TestIncidentsPluginFailureIsCorrelatedAndNeverStoresPayload`
- `TestIncidentsHTTPAuthenticationAndCrossRequestIdentity`
- `TestIncidentsBoundedPersistenceAndConcurrentAttribution`
- `TestIncidentsPersistenceFailureRemainsObservable`
- `TestIncidentsContextFailureAndCancellationAreDifferent`
- `TestIncidentsPanicKeepsSemanticsAndOmitsValue`
- `TestIncidentsDetectExplicitFailureResultWithoutChangingRPC`
- `TestIncidentsPluginLoadFaultAndMetadataBoundary`

</details>

<details><summary>internal/server/integrity_test.go · go · 5 个命名用例</summary>

[源码入口](../../internal/server/integrity_test.go)

- `TestIntegrityBaseline`
- `TestIntegrityMissingDir`
- `TestIntegrityCorruptedFile`
- `TestIntegrityTamperedBinary`
- `TestIntegrityRecoveryLogIsWritten`

</details>

<details><summary>internal/server/integrity_upgrade_test.go · go · 4 个命名用例</summary>

[源码入口](../../internal/server/integrity_upgrade_test.go)

- `TestIntegrityUpgradeReadinessAndDataPreservation`
- `TestIntegrityUpgradeRejectsUnverifiedTransitions`
- `TestIntegrityUpgradeRechecksAndPreservesOnFailure`
- `TestIntegrityUpgradeRollbackAndSubsequentTampering`

</details>

<details><summary>internal/server/issue_review_bounds_test.go · go · 12 个命名用例</summary>

[源码入口](../../internal/server/issue_review_bounds_test.go)

- `TestIssue72ConfigExportRejectsBadBodies`
- `TestIssue70SQLitePaginationBounds`
- `TestIssue62BriefingSortsInstantsBeforeTruncation`
- `TestIssue67VaultPersistenceFailureRollsBack`
- `TestIssue105SessionPersistenceRejectsUnsafeIDs`
- `TestIssue105StartupSkipsInvalidSessionWithoutDeletingIt`
- `TestIssue65PrereleaseOrdering`
- `TestIssue74RejectsRemoteWorkspaceBatchInjection`
- `TestIssue81FailedDirectoryRepairIsNotHealthy`
- `TestIssue96RejectsFractionalRevision`
- `TestIssue95BoundsAggregateProviderStream`
- `TestIssue111ImportCannotReportFailedSecretWritesAsSuccess`

</details>

<details><summary>internal/server/kdf_test.go · go · 9 个命名用例</summary>

[源码入口](../../internal/server/kdf_test.go)

- `TestArgon2idHashVerify`
- `TestPHCFormat`
- `TestLegacyDetection`
- `TestMigrationFlow`
- `TestReWrapCiphertext`
- `TestReWrapAllVoice`
- `TestDeriveAESKeyDeterministic`
- `TestEmptyPassword`
- `TestMigrationIntegration`

</details>

<details><summary>internal/server/knowledge_background_test.go · go · 9 个命名用例</summary>

[源码入口](../../internal/server/knowledge_background_test.go)

- `TestKnowledgeBackgroundCompletesWithoutFurtherRequests`
- `TestKnowledgeBackgroundOwnershipCancellation`
- `TestKnowledgeBackgroundRequestCancellationIsIndependent`
- `TestKnowledgeBackgroundCancelledFirstBatchResumes`
- `TestKnowledgeBackgroundFreezesCompletedSources`
- `TestKnowledgeBackgroundHardLimitDoesNotSpin`
- `TestKnowledgeBackgroundFirstBusyRequestIsQueued`
- `TestKnowledgeBackgroundStaleResponseContinues`
- `TestKnowledgeBackgroundSlowResponseDoesNotHoldScanGate`

</details>

<details><summary>internal/server/knowledge_code_budget_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/knowledge_code_budget_test.go)

- `TestKnowledgeCodeBudgetExcludesCatalogueEdges`

</details>

<details><summary>internal/server/knowledge_code_shadow_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/knowledge_code_shadow_test.go)

- `TestKnowledgeCodeLexicalBindings`
- `TestKnowledgeCodeSameLineAndScopedImports`
- `TestKnowledgeCodeScopedImports`

</details>

<details><summary>internal/server/knowledge_code_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/knowledge_code_test.go)

- `TestKnowledgeCodeConfidenceAndUnresolved`
- `TestKnowledgeCodeUnresolvedEvidenceBound`

</details>

<details><summary>internal/server/knowledge_conclusion_ai_test.go · go · 6 个命名用例</summary>

[源码入口](../../internal/server/knowledge_conclusion_ai_test.go)

- `TestKnowledgeConclusionAICaptureAndAttribution`
- `TestKnowledgeConclusionAIQuestionIsolation`
- `TestKnowledgeConclusionAIUnheadedAndIncomplete`
- `TestKnowledgeConclusionAIHistoryBeyondGraphPrefix`
- `TestKnowledgeConclusionAIUpgradePreservesEncryptedV1`
- `TestKnowledgeConclusionAIBudgetsAndUnboundQuestions`

</details>

<details><summary>internal/server/knowledge_conclusion_body_test.go · go · 6 个命名用例</summary>

[源码入口](../../internal/server/knowledge_conclusion_body_test.go)

- `TestKnowledgeConclusionBodyIndexedHTTPAndSavedSelection`
- `TestKnowledgeConclusionBodyCaptureAndTailSelection`
- `TestKnowledgeConclusionBodyBudgetsIntegrityAndLegacy`
- `TestKnowledgeConclusionBodyV2UpgradeAndEncryptedRoundTrip`
- `TestKnowledgeConclusionBodyDocumentTailAndStableLegacyToken`
- `TestKnowledgeConclusionBodyUpgradeFailurePreservesOriginal`

</details>

<details><summary>internal/server/knowledge_conclusion_catalog_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/knowledge_conclusion_catalog_test.go)

- `TestKnowledgeConclusionCatalogNonMarkdownSavedText`
- `TestKnowledgeConclusionCatalogLargeNodeFilterReachability`
- `TestKnowledgeConclusionCatalogUnavailableTextRemainsUnknown`

</details>

<details><summary>internal/server/knowledge_conclusion_evidence_test.go · go · 6 个命名用例</summary>

[源码入口](../../internal/server/knowledge_conclusion_evidence_test.go)

- `TestKnowledgeConclusionEvidenceChangesWithoutQuoteChange`
- `TestKnowledgeConclusionEvidenceReferencesAndUnknowns`
- `TestKnowledgeConclusionEvidenceScopeAndSyntax`
- `TestKnowledgeConclusionEvidenceAIExactQuestion`
- `TestKnowledgeConclusionEvidenceActualIndexDiskHTTP`
- `TestKnowledgeConclusionEvidenceCodeHashAndBudgets`

环境引用：`AIDE_EXPORT_CONCLUSION_FIXTURE`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/knowledge_conclusion_markdown_test.go · go · 10 个命名用例</summary>

[源码入口](../../internal/server/knowledge_conclusion_markdown_test.go)

- `TestKnowledgeConclusionMarkdownIndexedDiskHTTP`
- `TestKnowledgeConclusionMarkdownReferenceDefinitions`
- `TestKnowledgeConclusionMarkdownIndependentDefinitionChanges`
- `TestKnowledgeConclusionMarkdownSyntaxDoesNotInventUsage`
- `TestKnowledgeConclusionMarkdownAIBodyAndLegacyIsolation`
- `TestKnowledgeConclusionMarkdownManualSelectionContext`
- `TestKnowledgeConclusionMarkdownBudgetsAndUnknownAnchor`
- `TestKnowledgeConclusionMarkdownLegacySelectionDoesNotLoseCodeContext`
- `TestKnowledgeConclusionMarkdownEmptyDestinationCannotBindLiteralUsageFile`
- `TestKnowledgeConclusionMarkdownDefinitionPreservesOriginalCRLF`

</details>

<details><summary>internal/server/knowledge_conclusion_topics_browser_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/knowledge_conclusion_topics_browser_test.go)

- `TestKnowledgeConclusionTopicsLiveBrowser`

环境引用：`AIDE_TOPIC_BODY_FIXTURE`, `AIDE_TOPIC_BROWSER_FIXTURE`, `AIDE_TOPIC_REFERENCE_FIXTURE`, `AIDE_TOPIC_TEXT_FIXTURE`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/knowledge_conclusion_topics_test.go · go · 4 个命名用例</summary>

[源码入口](../../internal/server/knowledge_conclusion_topics_test.go)

- `TestKnowledgeConclusionSelectionUTF16AndRetitles`
- `TestKnowledgeConclusionTopicsHTTPPersistenceAndConflicts`
- `TestKnowledgeConclusionTopicsLockCancelAndSymlinks`
- `TestKnowledgeConclusionTopicsSelectedReplyExpiryAndCapacity`

</details>

<details><summary>internal/server/knowledge_conclusions_test.go · go · 8 个命名用例</summary>

[源码入口](../../internal/server/knowledge_conclusions_test.go)

- `TestKnowledgeConclusionComparisonWire`
- `TestKnowledgeConclusionExplicitCapture`
- `TestKnowledgeConclusionChangesAreScoped`
- `TestKnowledgeConclusionBounds`
- `TestKnowledgeConclusionHistoricalAPI`
- `TestKnowledgeConclusionSourceIdentity`
- `TestKnowledgeConclusionActualIndexHistory`
- `TestKnowledgeConclusionNoMixedSessionAttribution`

</details>

<details><summary>internal/server/knowledge_context_dedup_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/knowledge_context_dedup_test.go)

- `TestKnowledgeBuiltinContextIndexedOnceWithReadCompatibility`

</details>

<details><summary>internal/server/knowledge_coverage_test.go · go · 6 个命名用例</summary>

[源码入口](../../internal/server/knowledge_coverage_test.go)

- `TestKnowledgeCoverageSmallLocal`
- `TestKnowledgeCoverageFileBudget`
- `TestKnowledgeCoverageDirectoryBeyondLegacyThousandEntryLimit`
- `TestKnowledgeCoverageDirectoryBeyondDynamicEntryBudgetIsExplicit`
- `TestKnowledgeCoverageDepthBudget`
- `TestKnowledgeCoverageSessionLimit`

</details>

<details><summary>internal/server/knowledge_cursor_process_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/knowledge_cursor_process_test.go)

- `TestSSHKnowledgeCursorRestartHelper`

环境引用：`AIDE_TEST_CURSOR_PROCESS`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/knowledge_cursor_store_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/knowledge_cursor_store_test.go)

- `TestKnowledgeCursorRestart`
- `TestKnowledgeCursorInvalidPaths`

</details>

<details><summary>internal/server/knowledge_discovery_test.go · go · 9 个命名用例</summary>

[源码入口](../../internal/server/knowledge_discovery_test.go)

- `TestKnowledgeDiscoveryResumesLargeDirectory`
- `TestKnowledgeDiscoveryExcludedAndCancel`
- `TestKnowledgeDiscoveryReadAheadCloneAndResume`
- `TestKnowledgeDiscoveryCloneAndDirectoryMutation`
- `TestKnowledgeDiscoveryDepthAndCatalogLimits`
- `TestKnowledgeDiscoveryCatalogCanResume`
- `TestKnowledgeDiscoveryDirectPriority`
- `TestKnowledgeDiscoveryPriorityRejectsSymlinkAncestor`
- `TestKnowledgeDiscoveryScopedDirectAddressing`

</details>

<details><summary>internal/server/knowledge_document_continuation_test.go · go · 4 个命名用例</summary>

[源码入口](../../internal/server/knowledge_document_continuation_test.go)

- `TestKnowledgeDocumentContinuation`
- `TestKnowledgeDocumentContinuationKeepsAllOriginalMatches`
- `TestKnowledgeDocumentContinuationResumesInsideChunkLimitedFile`
- `TestKnowledgeDocumentLineLocations`

</details>

<details><summary>internal/server/knowledge_embedding_policy_test.go · go · 4 个命名用例</summary>

[源码入口](../../internal/server/knowledge_embedding_policy_test.go)

- `TestKnowledgeEmbeddingPolicyPersistence`
- `TestKnowledgeEmbeddingRAGIntegration`
- `TestKnowledgeEmbeddingRejectsChangedWorkspace`
- `TestKnowledgeEmbeddingCacheReuseAndInvalidation`

</details>

<details><summary>internal/server/knowledge_embeddings_test.go · go · 4 个命名用例</summary>

[源码入口](../../internal/server/knowledge_embeddings_test.go)

- `TestKnowledgeEmbeddingsBatchOrder`
- `TestKnowledgeEmbeddingsMalformed`
- `TestKnowledgeEmbeddingsCancelAndRedirect`
- `TestKnowledgeEmbeddingsDimensionAndDuplicateIndices`

</details>

<details><summary>internal/server/knowledge_ftp_live_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/knowledge_ftp_live_test.go)

- `TestKnowledgeFTPAndFTPSLiveAdapters`

</details>

<details><summary>internal/server/knowledge_ftp_progressive_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/knowledge_ftp_progressive_test.go)

- `TestKnowledgeListingPreservesLargeDirectory`
- `TestKnowledgeFTPProgressiveAdapter`

</details>

<details><summary>internal/server/knowledge_ftp_service_live_test.go · go · 0 个命名用例</summary>

[源码入口](../../internal/server/knowledge_ftp_service_live_test.go)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>internal/server/knowledge_go_types_test.go · go · 6 个命名用例</summary>

[源码入口](../../internal/server/knowledge_go_types_test.go)

- `TestKnowledgeGoTypesBindings`
- `TestKnowledgeGoTypesSingleMethodInterface`
- `TestKnowledgeGoTypesResolvedDiagnosticsByColumn`
- `TestKnowledgeGoTypesFailClosed`
- `TestKnowledgeGoTypeHelperBounds`
- `TestKnowledgeGoTypeProcessCancellationAndEnvironment`

</details>

<details><summary>internal/server/knowledge_hybrid_test.go · go · 4 个命名用例</summary>

[源码入口](../../internal/server/knowledge_hybrid_test.go)

- `TestKnowledgeHybridFusionPreservesEvidence`
- `TestKnowledgeHybridVectorValidation`
- `TestKnowledgeHybridRejectsStaleRankIndices`
- `TestKnowledgeHybridKeywordRetrievalCompatibility`

</details>

<details><summary>internal/server/knowledge_index_policy_test.go · go · 4 个命名用例</summary>

[源码入口](../../internal/server/knowledge_index_policy_test.go)

- `TestKnowledgeIndexPolicyPersistenceAndConflict`
- `TestKnowledgeIndexPriorityBudget`
- `TestKnowledgeIndexScopePersistenceAndGraph`
- `TestKnowledgeIndexLanguageServiceValidation`

</details>

<details><summary>internal/server/knowledge_local_failure_test.go · go · 8 个命名用例</summary>

[源码入口](../../internal/server/knowledge_local_failure_test.go)

- `TestKnowledgeCloudPlaceholderReadMessage`
- `TestKnowledgeLocalReadFailurePublishesPrefixAndOtherSources`
- `TestKnowledgeUnreadableBodyDoesNotHideLaterFiles`
- `TestKnowledgeLocalUnavailableRootRetainsSameOriginObservation`
- `TestKnowledgeLocalFallbackRejectsChangedSourceIdentity`
- `TestKnowledgeLocalFallbackDoesNotRestoreEvidenceForChangedText`
- `TestKnowledgeClosedAppCannotPublishRetainedObservation`
- `TestKnowledgeLocalFallbackBoundsNodesEdgesAndRetryMaterial`

</details>

<details><summary>internal/server/knowledge_local_store_test.go · go · 6 个命名用例</summary>

[源码入口](../../internal/server/knowledge_local_store_test.go)

- `TestKnowledgeLocalRestartAPI`
- `TestKnowledgeLocalProcessHelper`
- `TestKnowledgeLocalFreshProcess`
- `TestKnowledgeLocalInvalidCheckpoint`
- `TestKnowledgeLocalCursorCompressedLargeMaterialsAndLegacyRead`
- `TestKnowledgeLocalRestartAcrossCycles`

环境引用：`AIDE_TEST_LOCAL_CURSOR_PROCESS`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/knowledge_map_mentions_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/knowledge_map_mentions_test.go)

- `TestKnowledgeMentionLocationUsesUTF8LineAndColumn`
- `TestKnowledgeMentionLocationOmitsMissingToken`

</details>

<details><summary>internal/server/knowledge_material_queue_test.go · go · 4 个命名用例</summary>

[源码入口](../../internal/server/knowledge_material_queue_test.go)

- `TestKnowledgeMaterialTimeSliceLeavesExplicitPending`
- `TestKnowledgeMaterialBatchBoundsAndFairness`
- `TestKnowledgeMaterialQueueCheckpointAndClone`
- `TestKnowledgeMaterialCycleWaitsForValidation`

</details>

<details><summary>internal/server/knowledge_material_reuse_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/knowledge_material_reuse_test.go)

- `TestKnowledgeBatchMaterialAliasesReadEachBodyOnce`
- `TestKnowledgeBatchMaterialRevalidatesStampAndLocation`
- `TestKnowledgeBatchMaterialCodeUpgradeKeepsVerifiedText`

</details>

<details><summary>internal/server/knowledge_outline_test.go · go · 6 个命名用例</summary>

[源码入口](../../internal/server/knowledge_outline_test.go)

- `TestKnowledgeOutlineDeepHeadingAndBounds`
- `TestKnowledgeOutlineDeclarationCandidates`
- `TestKnowledgeOutlineCachePersistenceEditDelete`
- `TestKnowledgeOutlineAIProjection`
- `TestKnowledgeOutlineViewBudget`
- `TestKnowledgeOutlinePartialDigestNotFullFile`

</details>

<details><summary>internal/server/knowledge_path_fingerprint_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/knowledge_path_fingerprint_test.go)

- `TestKnowledgePathIndexedTextFingerprint`

</details>

<details><summary>internal/server/knowledge_paths_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/knowledge_paths_test.go)

- `TestKnowledgePathAPIRevisionIsolation`
- `TestKnowledgePathEvidence`
- `TestKnowledgePathLimitsAndCancellation`

</details>

<details><summary>internal/server/knowledge_progressive_test.go · go · 12 个命名用例</summary>

[源码入口](../../internal/server/knowledge_progressive_test.go)

- `TestKnowledgeProgressiveGraphAccumulatesAndRemoves`
- `TestKnowledgeProgressiveCatalogAccumulatesBeyondOneLargeDirectory`
- `TestKnowledgeProgressiveCancelledCandidateDoesNotAdvance`
- `TestKnowledgeDiscoveryBudgetIncreaseKeepsCursor`
- `TestKnowledgeProgressiveCatalogExpandsWhenInitialCapacityIsReached`
- `TestKnowledgeLocalDiscoveryRevisionIgnoresOnlyBudgets`
- `TestKnowledgeProgressiveUpdateAPI`
- `TestKnowledgeUpdatesServePublishedGraphWhileScanIsBusy`
- `TestKnowledgeUpdatesKeepPublishedGraphWhenRefreshFailsAndResumeCache`
- `TestKnowledgeRefreshProgressOverlayKeepsPublishedGraphImmutable`
- `TestKnowledgeProgressiveRejectsReplacedAncestor`
- `TestKnowledgeCancelledRefreshRetainsUnvisitedMaterial`

</details>

<details><summary>internal/server/knowledge_reference_progressive_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/knowledge_reference_progressive_test.go)

- `TestKnowledgeReferenceRepositoryProgressive`

环境引用：`AIDE_TEST_REFERENCE_REPOSITORY`, `AIDE_TEST_REFERENCE_SOURCE_PATH`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/knowledge_references_equivalence_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/knowledge_references_equivalence_test.go)

- `TestKnowledgeReferenceIndexedEquivalence`
- `TestKnowledgeReferenceLargeRepositoryWork`
- `TestKnowledgeReferenceHierarchyDoesNotConsumeMentionBudget`

</details>

<details><summary>internal/server/knowledge_references_test.go · go · 6 个命名用例</summary>

[源码入口](../../internal/server/knowledge_references_test.go)

- `TestKnowledgeReferenceSpacedReportSourcePath`
- `TestKnowledgeReferenceLocatorUsesMatchedOccurrence`
- `TestKnowledgeReferenceNamespacedSuffixIsNotReference`
- `TestKnowledgeReferenceEncodingsAndAreaBoundaries`
- `TestKnowledgeReferenceExtractionBounds`
- `TestKnowledgeReferenceIndexedReportPathAPI`

</details>

<details><summary>internal/server/knowledge_remote_progressive_test.go · go · 7 个命名用例</summary>

[源码入口](../../internal/server/knowledge_remote_progressive_test.go)

- `TestKnowledgeRemoteProgressiveCursor`
- `TestKnowledgeRemoteProgressiveCodeBudget`
- `TestKnowledgeRemoteProgressiveCodeCount`
- `TestKnowledgeRemoteProgressiveReadFailure`
- `TestKnowledgeRemoteProgressiveDirectoryMutation`
- `TestKnowledgeRemoteProgressiveLimits`
- `TestKnowledgeRemoteProgressiveYieldsAndResumes`

</details>

<details><summary>internal/server/knowledge_shared_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/knowledge_shared_test.go)

- `TestKnowledgeSharedSnapshotSearchAndUpdates`
- `TestKnowledgeSharedCancellationAndConcurrency`

</details>

<details><summary>internal/server/knowledge_startup_fifo_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/knowledge_startup_fifo_test.go)

- `TestKnowledgeStartupUnavailableFIFOIsNeverOpened`
- `TestKnowledgeIndependentBlockedSourceDoesNotBlockOtherSources`

</details>

<details><summary>internal/server/knowledge_startup_recovery_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/knowledge_startup_recovery_test.go)

- `TestKnowledgeStartupUnavailableSourceDoesNotRetarget`

</details>

<details><summary>internal/server/knowledge_timeline_codec_test.go · go · 5 个命名用例</summary>

[源码入口](../../internal/server/knowledge_timeline_codec_test.go)

- `TestKnowledgeTimelineLargeCompressedRetention`
- `TestKnowledgeTimelineLegacyCompressionMigration`
- `TestKnowledgeTimelineCompressionRejectsCorruptionAndBomb`
- `TestKnowledgeTimelineIndependentCapacityBounds`
- `TestKnowledgeTimelineExistingV2ReaderCompatibility`

</details>

<details><summary>internal/server/knowledge_timeline_test.go · go · 5 个命名用例</summary>

[源码入口](../../internal/server/knowledge_timeline_test.go)

- `TestKnowledgeTimelinePersistenceEvidence`
- `TestKnowledgeTimelineRetentionAndCorruption`
- `TestKnowledgeTimelineHandlerIsolation`
- `TestKnowledgeTimelineCanonicalAndCapacity`
- `TestKnowledgeTimelineUpdatesRestartAndEpoch`

</details>

<details><summary>internal/server/knowledge_vector_cache_test.go · go · 4 个命名用例</summary>

[源码入口](../../internal/server/knowledge_vector_cache_test.go)

- `TestKnowledgeVectorCachePersistenceAndIsolation`
- `TestKnowledgeVectorChunkKeyTracksContent`
- `TestKnowledgeVectorCacheBoundsAndCorruption`
- `TestKnowledgeVectorCacheConcurrent`

</details>

<details><summary>internal/server/knowledge_web_helpers_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/knowledge_web_helpers_test.go)

- `TestKnowledgeWebHelperPathIsolation`
- `TestKnowledgeWebRootScriptAmbiguity`

</details>

<details><summary>internal/server/knowledge_web_test.go · go · 6 个命名用例</summary>

[源码入口](../../internal/server/knowledge_web_test.go)

- `TestKnowledgeWebParserTransport`
- `TestKnowledgeWebHTMLParserTransport`
- `TestKnowledgeWebGoRouteDeclarations`
- `TestKnowledgeWebIndexedPathAPI`
- `TestKnowledgeWebButtonServicePath`
- `TestKnowledgeWebRouteAndPageIsolation`

</details>

<details><summary>internal/server/large_engineering_benchmark_test.go · go · 4 个命名用例</summary>

[源码入口](../../internal/server/large_engineering_benchmark_test.go)

- `TestLargeEngineeringFixtureOracle`
- `TestLargeEngineeringVerifierUsesCommonJSRealm`
- `TestLargeEngineeringReportProposal`
- `TestLargeEngineeringLiveLuna`

环境引用：`AIDE_LARGE_LIVE_ALL_SLOTS`, `AIDE_LARGE_LIVE_DATA`, `AIDE_LARGE_LIVE_ONLY`, `AIDE_LARGE_LIVE_PROOF`, `AIDE_LARGE_LIVE_WORK_QUEUE`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/large_transfer_browser_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/large_transfer_browser_test.go)

- `TestLargeTransferBrowser`

环境引用：`AIDE_EVIDENCE_DIR`, `AIDE_LARGE_TRANSFER_BROWSER`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/length_continue_test.go · go · 4 个命名用例</summary>

[源码入口](../../internal/server/length_continue_test.go)

- `TestIncompleteToolCallIndex`
- `TestStripCodeFence`
- `TestToolLoopLengthTruncatedToolCallAutoPatches`
- `TestToolLoopLengthPureTextContinues`

</details>

<details><summary>internal/server/local_mounts_test.go · go · 6 个命名用例</summary>

[源码入口](../../internal/server/local_mounts_test.go)

- `TestLocalMountSettingsRevisionAndActions`
- `TestLocalMountRootsBoundaryAndWorkspaceRights`
- `TestLocalMountRejectsOutsideSymlink`
- `TestLocalMountAnchoredOpeningAndMissingCache`
- `TestLocalMountWorkspaceKeepsCacheInternal`
- `TestLocalMountExternalSystemDocumentsRoundTrip`

</details>

<details><summary>internal/server/lock_browser_fixture_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/lock_browser_fixture_test.go)

- `TestLockBrowserFixture`

环境引用：`AIDE_LOCK_BROWSER_FIXTURE`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/lock_state_test.go · go · 9 个命名用例</summary>

[源码入口](../../internal/server/lock_state_test.go)

- `TestLockStateMissingDefaultsUnlocked`
- `TestLockStateWriteReadRoundtripAndGen`
- `TestLockStateFileMode0600`
- `TestLockStateCorruptedFallsBackUnlocked`
- `TestLockStateHandlers`
- `TestLockStateCorruptWithPasswordFailsClosed`
- `TestLockStateCannotUnlockWithoutVerification`
- `TestLockStateVerifiedGenerationCannotClearNewLock`
- `TestLockStateWithoutPasswordDoesNotTrapStarMap`

</details>

<details><summary>internal/server/lunar_calendar_plugin_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/lunar_calendar_plugin_test.go)

- `TestLunarCalendarPluginAvailableToAideAndXiaomi`

</details>

<details><summary>internal/server/management_test.go · go · 6 个命名用例</summary>

[源码入口](../../internal/server/management_test.go)

- `TestManagementPermissionsPersistAndRoles`
- `TestAuthoringPrepareDoesNotExecuteAndApprovalInstalls`
- `TestManagementUpgradePreservesSettingsAndDisabledState`
- `TestManagementRevocationAndStaleState`
- `TestManagementReferenceLocksAndOrdinaryAccess`
- `TestAuthoringRejectsInvalidCodeAndPaths`

</details>

<details><summary>internal/server/markdown_backup_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/markdown_backup_test.go)

- `TestMarkdownBackupCompleteAndVerified`
- `TestMarkdownBackupBudget`

</details>

<details><summary>internal/server/markdown_history_test.go · go · 9 个命名用例</summary>

[源码入口](../../internal/server/markdown_history_test.go)

- `TestMarkdownHistoryObservedAssetsAndDedup`
- `TestMarkdownHistoryRejectsCorruptObjects`
- `TestMarkdownHistoryReadPinsWorkspace`
- `TestMarkdownRestoreBundleAndConflict`
- `TestMarkdownRestoreUnavailableAssetAndReadOnlySource`
- `TestMarkdownHistoryPartialWriteContract`
- `TestHistoryObjectReuseVerifiesBytesAndKind`
- `TestHistoryObjectValidReuseIsImmutable`
- `TestMarkdownHistoryInvalidBackupBlocksWrite`

</details>

<details><summary>internal/server/markdown_manage_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/markdown_manage_test.go)

- `TestMarkdownRetentionKeepsLatestAndStableIDs`
- `TestMarkdownManageImportRetentionAndGarbage`

</details>

<details><summary>internal/server/market_cache_binding_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/market_cache_binding_test.go)

- `TestIssue89CachedManifestBoundToCatalogIdentity`

</details>

<details><summary>internal/server/master_password_limit_test.go · go · 4 个命名用例</summary>

[源码入口](../../internal/server/master_password_limit_test.go)

- `TestIssue115MasterPasswordBudgetSharedAcrossUnlockRoutes`
- `TestIssue115PasswordBudgetExpiresWithoutRejectedAttemptsExtendingLockout`
- `TestIssue115ParallelAttemptsCannotOverrunSharedBudget`
- `TestIssue115PasswordlessRoutesDoNotConsumePasswordBudget`

</details>

<details><summary>internal/server/mcp_existing_configuration_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/mcp_existing_configuration_test.go)

- `TestMCPExistingHTTPConfigurationRoundTrip`

</details>

<details><summary>internal/server/mcp_http_test.go · go · 7 个命名用例</summary>

[源码入口](../../internal/server/mcp_http_test.go)

- `TestMCPHTTPSourceDiscoveryAndReadOnlyCall`
- `TestMCPHTTPRejectsUnsafeURLAndMixedCommand`
- `TestMCPHTTPAuthRedirectAndBoundedResponses`
- `TestMCPHTTPCancellationDoesNotWaitForGateway`
- `TestDockerMCPGatewayVirtualHostRequiresPrivateEndpointAndToken`
- `TestMCPHTTPRejectsStaleConnectionBeforeSending`
- `TestMCPHTTPRevocationAndDiscoveryCompareAndSwap`

</details>

<details><summary>internal/server/memory_access_test.go · go · 8 个命名用例</summary>

[源码入口](../../internal/server/memory_access_test.go)

- `TestAideCannotReadAssistantMemory`
- `TestAideCannotWriteAssistantMemory`
- `TestAssistantCanReadAideMemory`
- `TestAssistantCannotWriteAideMemory`
- `TestProjectAideMemoryFollowsWorkspace`
- `TestLegacyGlobalAideMemoryMigratesOnceToProjectCache`
- `TestIsolationPolicy`
- `TestShellTouchesAssistantZone`

</details>

<details><summary>internal/server/memory_tokens_test.go · go · 5 个命名用例</summary>

[源码入口](../../internal/server/memory_tokens_test.go)

- `TestMemoryTokensLegacyAndExternalEdit`
- `TestMemoryTokensCorruptionAndLimits`
- `TestMemoryTokensStableLegacyAndUnicode`
- `TestMemoryTokensRejectsLinkedMemory`
- `TestConcurrentProjectMemoryWritesRemainReadableAndConsistent`

</details>

<details><summary>internal/server/message_center_test.go · go · 6 个命名用例</summary>

[源码入口](../../internal/server/message_center_test.go)

- `TestMessageCenterOptInStableVaultIdentityAndBackupExclusion`
- `TestMessageCenterRequiresAuthUnlockAndLeavesReminderCRUD`
- `TestMessageCenterDoesNotReplaceExistingWorkspaceFile`
- `TestMessageCenterFailedNetworkConfigRestoresTurnCredential`
- `TestMessageCenterAccountNameAndAssistantPermission`
- `TestMessageCenterDockerBridgeIsLauncherOwned`

</details>

<details><summary>internal/server/message_lock_generation_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/message_lock_generation_test.go)

- `TestIssue85DelayedMessageMutationRejectsLockTransition`
- `TestIssue85LockCancelsAllLeasesBeforePublishingGeneration`
- `TestIssue85CleanupRemainsAvailableWhileLocked`

</details>

<details><summary>internal/server/message_notifications_test.go · go · 4 个命名用例</summary>

[源码入口](../../internal/server/message_notifications_test.go)

- `TestMessageNotificationMutationRejectsChangedLockGeneration`
- `TestMessageNotificationPreferencesRevisionPersistenceAndLock`
- `TestMessageNotificationAggregationDedupScopeAndMuteExpiry`
- `TestMessageNotificationGroupVoiceInheritsGroupPolicy`

</details>

<details><summary>internal/server/migration_journal_failure_test.go · go · 9 个命名用例</summary>

[源码入口](../../internal/server/migration_journal_failure_test.go)

- `TestIssue94FlatMigrationRejectsUnavailableJournalBeforeMutating`
- `TestIssue94InterruptedMigrationRestoresOriginalsAndPriorState`
- `TestIssue94FailedRollbackRetainsJournalForRetry`
- `TestIssue94CorruptPreimageIsNeverPublished`
- `TestIssue94CommittedJournalNeverUndoesCompletedMigration`
- `TestIssue94RecoveryRejectsEscapingMetadata`
- `TestIssue94MigrationEntryPointsRecoverBeforeNoOp`
- `TestIssue94ManualRecoveryDoesNotAlsoRollbackPreviousMigration`
- `TestIssue94SessionMigrationRejectsUnavailableJournalBeforeMutating`

</details>

<details><summary>internal/server/migration_test.go · go · 5 个命名用例</summary>

[源码入口](../../internal/server/migration_test.go)

- `TestDetectLayout`
- `TestMigration`
- `TestMigrationIdempotent`
- `TestMigrationRollback`
- `TestMigrationCorruptSessionKept`

</details>

<details><summary>internal/server/mixsoul/dynamics.test.cjs · node · 15 个命名用例</summary>

[源码入口](../../internal/server/mixsoul/dynamics.test.cjs)

- `age materially changes rank and eligibility; dormant records remain recoverable and encrypted`
- `importance/confidence affect competition; strong relevance can outweigh importance`
- `source-backed model importance is bounded; missing reasons and invented evidence cannot raise it`
- `explicit confirmation reinforces with evidence, rate limits, and survives reopening`
- `unexposed IDs, vague statements and expired evidence cannot reinforce`
- `explicit correction supersedes old evidence; completed promises stop automatic recall`
- `explicit expiry and supersession exclude even protected records`
- `time context survives reopen and is identical in text and native channels`
- `clock rollback is propagated without reducing the persisted high-water mark`
- `expression feedback gradually relaxes; no-store statements do not train habits`
- `v1 encrypted state migrates with an immutable encrypted recovery snapshot`
- `native observations learn once through the same bounded consolidation path`
- `ordinary recall never activates dormant records; explicit old-memory recall can recover them`
- `negated confirmation cannot strengthen old beliefs, and corrected pinned facts stay pinned`
- `stale source evidence cannot be replayed to learn or reinforce later`

环境引用：`MIXSOUL_TEST_REPORT`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/mixsoul/embedding-auth.test.cjs · node · 2 个命名用例</summary>

[源码入口](../../internal/server/mixsoul/embedding-auth.test.cjs)

- `local embedding bearer is bounded, sent only to literal loopback, and never in payload`
- `MIXSoul exposes followup cleanup only when the host provides it`

</details>

<details><summary>internal/server/mixsoul/emotion.test.cjs · node · 19 个命名用例</summary>

[源码入口](../../internal/server/mixsoul/emotion.test.cjs)

- `current self-report is bounded and third-party/past/negative evidence is not today\`
- `inferred feelings remain tentative; invented quote/label/ID and past/future claims rejected`
- `explicit affect links only exposed IDs; it never strengthens a linked fact or changes habits`
- `emotion and source survive encrypted reopening; future conversation never inherits old mood`
- `no-store and tentative evidence cannot create inferred emotional biography`
- `appraisal source is idempotent and rejects stale/mismatched evidence`
- `historical emotion decays, never gains weight on recall/pulse, and is deleted with source`
- `deleting a linked fact prunes affect links without inventing a replacement`
- `do not mention boundary suppresses past links and personality inference is rejected`
- `text appraisal changes the actual next dialogue prompt, with no tools during appraisal`
- `appraisal provider failure degrades to existing evidence, no silent tool dispatch`
- `native context uses immediate evidence, learning adds an event, time-only refresh has no current mood`
- `malformed/primitive appraisal cannot crash dialogue or invent a label`
- `a current positive emotion is not reversed by a model-selected old negative record`
- `questions and explicitly fictional self-statements are not stored as current affect`
- `emotional confirmation alone cannot reinforce an exposed factual memory`
- `a remembered ID deleted between retrieval and appraisal cannot receive an emotion link`
- `negation in the previous clause does not suppress a new explicit calm self-report`
- `an extra unsupported emotion cannot discard a supported emotion and valid event link`

</details>

<details><summary>internal/server/mixsoul/initiative.test.cjs · node · 8 个命名用例</summary>

[源码入口](../../internal/server/mixsoul/initiative.test.cjs)

- `idle candidate is grounded, one shot, no tools, and does not create user facts`
- `native candidate cannot leak into another call or idle channel`
- `quiet, resume and deletion invalidate queued initiative without erasing user memories`
- `new topic while generating discards the old autonomous answer`
- `no-store, closed answer and question do not schedule a follow-up`
- `empty clock pulses cannot invent initiative or call a model`
- `generation budgets and cooldown survive encrypted reopen; expired claims are silent`
- `multiple named projects cannot provide an autonomous topic`

</details>

<details><summary>internal/server/mixsoul/native-memory.test.cjs · node · 10 个命名用例</summary>

[源码入口](../../internal/server/mixsoul/native-memory.test.cjs)

- `native turns retain self advice and explicit affect across independent agent invocations`
- `native projects do not share advice, format or historical affect`
- `native no-store prevents both sides and deletion prevents later learning`
- `split native project, affect and request survive new-call recall without chat history`
- `false missing-memory answer is replaced only by scoped, exact advice evidence`
- `native project switches, explicit boundaries and new sockets do not inherit prior affect`
- `native no-store boundary is sticky across ASR fragments and survives agent process boundaries`
- `emotion wording guard softens hypotheses but preserves self-report, quotes, code and numbers`
- `accepted split affect is retrievable after hangup even without a completed assistant reply`
- `out-of-order ASR cannot inherit or overwrite the newer project scope`

</details>

<details><summary>internal/server/mixsoul/quotation.test.cjs · node · 4 个命名用例</summary>

[源码入口](../../internal/server/mixsoul/quotation.test.cjs)

- `colloquial original wording chooses only a scoped unsolicited assistant quote`
- `ambiguous, sensitive or long historical quotes do not become a literal plan`
- `ordinary recall replies do not push requested unsolicited evidence out of retrieval`
- `requesting the proactive reply and its preceding advice retains both evidence entries`

</details>

<details><summary>internal/server/mixsoul/store.test.cjs · node · 8 个命名用例</summary>

[源码入口](../../internal/server/mixsoul/store.test.cjs)

- `encrypted persistence, wrong key/scope and modified data rejected`
- `pulse advances real elapsed time but does not create experiences or confirmations`
- `feedback bounded, idempotent turn, ephemeral state not personality diagnosis`
- `consolidation requires current evidence; forgetting removes dependent vectors`
- `lexical vectors tagged and normalized; expired entries excluded`
- `agent owns dialogue and consolidation flow; no key leaked to messages`
- `explicit no-store and native context reuse`
- `named embedding spaces remain separate and version transitions exclude old facts`

</details>

<details><summary>internal/server/mixsoul/system.test.cjs · node · 25 个命名用例</summary>

[源码入口](../../internal/server/mixsoul/system.test.cjs)

- `ten equivalent expression paraphrases share one sourced memory; opposite/entity preferences stay separate`
- `idle jobs merge only known equivalent preferences, survive reopen, and allow undo without reinforcement`
- `current corrections exclude old version; past, third-party and temporary statements cannot create current preferences`
- `expiry must have literal date evidence and expires protected promises, not invented model dates`
- `persistent mention boundary hides source and derived memories across reopen; allow and delete are distinct`
- `salience feedback changes retention only with literal matching evidence; no-store never changes it`
- `context budget reserves promise/preferences, removes derived source repetition and bounds history`
- `contextual expression overrides habits without permanently changing them`
- `local semantic baseline and replaceable named spaces never mix query dimensions or versions`
- `6000 encrypted records reopen beyond previous limit; atomic manifest retains old generation on failed write`
- `authenticated chunk corruption fails closed and management uses explicit sourced correction`
- `text and native contexts enforce the same suppression and budget, no-store wording policy is in prompt`
- `persistent suppression filters older user and assistant chat messages, not just vector results`
- `new explicit expression preference replaces the opposite current value without erasing provenance`
- `loopback embedding adapter performs bounded resumable indexing and own-space search; remote URLs rejected`
- `named vectors reject wrong counts and local model eval has no remote fallback`
- `completion deadline makes an unresolved promise overdue, never silently expired`
- `negated wish and context-specific feedback cannot collapse into the positive preference`
- `expired derived evidence cannot return through a raw source episode`
- `owner management can inspect provenance without vectors and ID deletion removes source-based resurrection`
- `obligations are considered even when many unrelated high-score preferences fill the first candidate page`
- `dedup keeps all evidence roots beyond adjustment-history limit so deletion cannot resurrect an old source`
- `management page stays within host IPC budget and correction fetches full original text, never its snippet`
- `optional runtime embeddings reach text/native prompts, persist vectors, and idle pulse stays zero-call`
- `assistant dialogue is encrypted provenance evidence, never a user fact or independent emotional proof`

</details>

<details><summary>internal/server/mixsoul/topic.test.cjs · node · 7 个命名用例</summary>

[源码入口](../../internal/server/mixsoul/topic.test.cjs)

- `shared markers and generic project concepts cannot cross named-project boundaries`
- `same-project evidence strips one-turn presentation, explicit original recall preserves it`
- `vague recall with several known projects requests clarification, never the latest arbitrary row`
- `host history is scoped as user/assistant pairs and old numbering is historical data`
- `deleting an evidence dependency also clears later assistant turns that used it`
- `unscoped stable preferences remain available while project-scoped ones stay local`
- `project parsing supports exact named and quoted labels, never fuzzy aliases`

</details>

<details><summary>internal/server/mobile_capture_test.go · go · 10 个命名用例</summary>

[源码入口](../../internal/server/mobile_capture_test.go)

- `TestMobileCaptureMutualApprovalRetriesAndRevoke`
- `TestMobileCaptureConcurrentDeviceZonesAndCountVersions`
- `TestMobileCaptureTLSOriginScopeAndExpiry`
- `TestMobileCaptureNotAdminAndNoPrivateDataBeforeApproval`
- `TestMobileCaptureControlOptInReplayAndRenewal`
- `TestMobileCaptureLimitsAndRegionImmutability`
- `TestMobileCaptureStrictJSONAndPageHeaders`
- `TestMobileCaptureExplicitZeroCrossScopeAndLock`
- `TestMobileCaptureUnknownAndLocations`
- `TestMobileCaptureOrganizationPersonBinding`

</details>

<details><summary>internal/server/model_led_agent_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/model_led_agent_test.go)

- `TestModelLedWorkflowCompatibility`
- `TestModelLedPlanValidationAndJSONPersistence`
- `TestModelLedLoopPlansReceiptsAndSingleReview`

</details>

<details><summary>internal/server/models_test.go · go · 8 个命名用例</summary>

[源码入口](../../internal/server/models_test.go)

- `TestMultiModelSettings`
- `TestReasoningEffortCanBeUpdatedFromSettings`
- `TestSettingsMigrationFromSingleModel`
- `TestLegacySingleModelSettingsWriteUsesDefaultProvider`
- `TestModelListProxy`
- `TestMultipleModelProvidersUseTheirOwnVaultKeys`
- `TestTaskRecordsModel`
- `TestModelDiscoveryDraft`

</details>

<details><summary>internal/server/native_editor_readonly_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/native_editor_readonly_test.go)

- `TestIssue93ReadOnlyPermanentlyRevokesOpenedEditor`
- `TestIssue93ReadOnlyDisconnectsExistingEditorWebSocket`
- `TestIssue93FailedSettingsSaveKeepsWritableLease`

环境引用：`AIDE_BRDPLAN_EDITOR_URL`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/native_editor_target_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/native_editor_target_test.go)

- `TestNativeEditorTargetBoundToConfiguredWorkspace`
- `TestNativeEditorTargetRejectsAliasesAndMalformedSession`
- `TestNativeEditorTargetRejectsMultipleProjectsAndMissingGeneration`

</details>

<details><summary>internal/server/native_editor_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/native_editor_test.go)

- `TestNativeEditorOpaqueViewerAndScopeRevocation`
- `TestNativeEditorAdmissionRejectsInFlightAndReadOnly`
- `TestNativeEditorPersistentUnknownBlocksHostWrites`

</details>

<details><summary>internal/server/office_artifact_test.go · go · 5 个命名用例</summary>

[源码入口](../../internal/server/office_artifact_test.go)

- `TestOfficeCreateAndXlsxEdit`
- `TestReadFileRawBackupUsesUnderlyingMIME`
- `TestReadFileRawStepMIME`
- `TestOfficeXlsxReferencePermissions`
- `TestOfficeXlsxViewSparseSecondSheet`

</details>

<details><summary>internal/server/office_bounds_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/office_bounds_test.go)

- `TestIssue77LegacyOfficeAdmission`
- `TestIssue77OfficeParserMemoryLimit`

</details>

<details><summary>internal/server/office_comments_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/office_comments_test.go)

- `TestOfficeNativeCommentsRoundTrip`
- `TestOfficeNativeCommentsReferenceReadOnly`

</details>

<details><summary>internal/server/outcome_staged_test.go · go · 4 个命名用例</summary>

[源码入口](../../internal/server/outcome_staged_test.go)

- `TestOutcomeSavedStageVisibleBeforeFinalContent`
- `TestOutcomeStageSnapshotDetachedAndRejectsStalePages`
- `TestOutcomeStagePreviewBudgetStillBindsHiddenText`
- `TestOutcomeStagesDoNotPromoteNonChatOrSystemText`

</details>

<details><summary>internal/server/parallel_coding_benchmark_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/parallel_coding_benchmark_test.go)

- `TestCodingBenchmarkFixture`

</details>

<details><summary>internal/server/parallel_edit_drafts_atomic_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/parallel_edit_drafts_atomic_test.go)

- `TestParallelEditDraftsRollbackWholeBatchOnCancellation`

</details>

<details><summary>internal/server/parallel_edit_drafts_test.go · go · 4 个命名用例</summary>

[源码入口](../../internal/server/parallel_edit_drafts_test.go)

- `TestParallelEditDraftValidation`
- `TestParallelEditDraftsRequireReadsAndNormalApproval`
- `TestParallelEditEligibilityPreservesPolicy`
- `TestParallelEditDraftBatchRejectsBeforeRegistration`

</details>

<details><summary>internal/server/parallel_engineering_benchmark_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/parallel_engineering_benchmark_test.go)

- `TestEngineeringBenchmarkOracle`

</details>

<details><summary>internal/server/paths_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/paths_test.go)

- `TestPathsLayout`
- `TestEnsureDirsCreatesAllLayeredDirs`
- `TestPathsPureFunctionsNoSideEffect`

</details>

<details><summary>internal/server/personality_bounds_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/personality_bounds_test.go)

- `TestIssue80PersonalityAdmissionDoesNotMutateOnFailure`
- `TestIssue80PersonalityUnicodeBoundary`

</details>

<details><summary>internal/server/personality_evolution_test.go · go · 19 个命名用例</summary>

[源码入口](../../internal/server/personality_evolution_test.go)

- `TestEvolutionDisablesInheritedReasoning`
- `TestEvolutionProviderFailurePreservesPersonality`
- `TestEvolutionLengthRepair`
- `TestEvolutionCountsUnicodeCharacters`
- `TestTriggerAfterMessageCount`
- `TestTriggerPersistence`
- `TestEvolveShorterKeepsCore`
- `TestEvolvePersonalityUsesVaultAPIKey`
- `TestEvolveLongerWithinThreshold`
- `TestEvolveLongerBeyondThreshold`
- `TestEvolveCoreLost`
- `TestEvolveTooSimilar`
- `TestCompressModeStrictShorter`
- `TestEndToEndEvolution`
- `TestAuditOnAttempt`
- `TestInsufficientSampleSkip`
- `TestRollback`
- `TestReset`
- `TestManualEvolveEndpoint`

</details>

<details><summary>internal/server/personality_persistence_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/personality_persistence_test.go)

- `TestIssue155PersonalityChangesRejectPersistenceFailure`
- `TestIssue155MissingStateIsRemovedWhenSettingsSaveFails`
- `TestIssue155FailedMetadataSaveKeepsPreviousState`

</details>

<details><summary>internal/server/personality_snapshot_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/personality_snapshot_test.go)

- `TestIssue101EvolutionCannotOverwriteInterveningEdits`
- `TestIssue101OverlappingEvolutionOnlyAdoptsCurrentDraft`
- `TestIssue101OtherPersonalityEditDoesNotInvalidateDraft`

</details>

<details><summary>internal/server/plugin_call_queue_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/plugin_call_queue_test.go)

- `TestPluginCallQueueFIFOAndCancellation`
- `TestPluginCallsSerializeSharedFileWrites`
- `TestPluginCallsDifferentPluginsOverlap`

</details>

<details><summary>internal/server/plugin_commerce_test.go · go · 5 个命名用例</summary>

[源码入口](../../internal/server/plugin_commerce_test.go)

- `TestPluginCommercePrivateInstallAndUpdate`
- `TestPluginCommerceRejectInvalidLicense`
- `TestPluginCommerceCatalogAndPublicIsolation`
- `TestPluginCommerceExtendedCatalogKeepsLegacyPublicEntries`
- `TestPluginCommerceIdentitySurvivesRestart`

</details>

<details><summary>internal/server/plugin_daemon_test.go · go · 12 个命名用例</summary>

[源码入口](../../internal/server/plugin_daemon_test.go)

- `TestDaemonStartStop`
- `TestDaemonCallViaIPC`
- `TestDaemonCrashRestart`
- `TestDaemonEventReporting`
- `TestNonDaemonUnchanged`
- `TestDaemonDisabledByDefault`
- `TestDaemonStartOutsideGlobalLock`
- `TestDaemonStopOutsideGlobalLock`
- `TestDaemonConcurrentStartStopConsistency`
- `TestDaemonConcurrentStartsShareSupervisor`
- `TestDaemonTimeoutStopsHandlerBeforeReturning`
- `TestDaemonTimeoutDoesNotStopReplacementGeneration`

</details>

<details><summary>internal/server/plugin_distribution_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/plugin_distribution_test.go)

- `TestIndependentPluginsDoNotInstallFromCheckoutOrLegacyImage`
- `TestIndependentPluginsPreserveInstalledStateAcrossReload`

</details>

<details><summary>internal/server/plugin_effect_checks_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/plugin_effect_checks_test.go)

- `TestPluginEffectCheckPersistenceAndTruthBoundaries`
- `TestPluginEffectCheckRejectsStaleInvalidAndActive`
- `TestPluginEffectCheckConcurrencySaveFailureAndLimit`

</details>

<details><summary>internal/server/plugin_experience_trust_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/plugin_experience_trust_test.go)

- `TestIssue91PluginResultsDoNotPersistAsFutureInstructions`
- `TestIssue91LegacyDetailsAndMalformedIdentitiesCannotPoisonHint`

</details>

<details><summary>internal/server/plugin_external_test.go · go · 0 个命名用例</summary>

[源码入口](../../internal/server/plugin_external_test.go)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_PLUGIN_TEST_ROOT`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/plugin_file_browser_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/plugin_file_browser_test.go)

- `TestPluginFilePreviewBrowser`

环境引用：`AIDE_PLUGIN_FILE_BROWSER`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/plugin_file_handlers_test.go · go · 12 个命名用例</summary>

[源码入口](../../internal/server/plugin_file_handlers_test.go)

- `TestPluginFileHandlerManifestResolution`
- `TestPluginDocumentRangeAssetsAndScope`
- `TestPluginDocumentPackageAssetsStayInOwnPackage`
- `TestPluginDocumentBinarySaveVersionAndCleanup`
- `TestPluginDocumentConflictingSavePreservesFile`
- `TestPluginDocumentReadOnlyReferenceAndKnowledge`
- `TestPluginDocumentCloseAndWorkspaceRevocation`
- `TestPluginDocumentRenewPreservesDraftAndRevokesOldKey`
- `TestPluginDocumentReferenceRevocationPreservesOriginal`
- `TestPluginDocumentLargeOffsetRange`
- `TestPluginDocumentSFTPAtomicSave`
- `TestPluginDocumentAppCloseCancelsInFlightRead`

</details>

<details><summary>internal/server/plugin_host_component_test.go · go · 4 个命名用例</summary>

[源码入口](../../internal/server/plugin_host_component_test.go)

- `TestHostComponentProjectionAndRoutes`
- `TestHostComponentMetadata`
- `TestHostComponentScopeReplayAndMissingHost`
- `TestHostComponentQuotaProjection`

</details>

<details><summary>internal/server/plugin_images_test.go · go · 5 个命名用例</summary>

[源码入口](../../internal/server/plugin_images_test.go)

- `TestPluginImagesValidateAndDoNotLeakIntoText`
- `TestPluginImagesProviderGate`
- `TestPluginImagesActualToolLoopWire`
- `TestPluginImagesProtocolAndRoundBudget`
- `TestPluginImagesAutoRouteDoesNotGateOnVisionMetadata`

</details>

<details><summary>internal/server/plugin_learning_test.go · go · 6 个命名用例</summary>

[源码入口](../../internal/server/plugin_learning_test.go)

- `TestPluginCapabilityHint`
- `TestPluginExperienceLifecycle`
- `TestParseExperienceLine`
- `TestSanitizeExpDetail`
- `TestTrimExperience`
- `TestRecordPluginExperienceIgnoresNonPlugin`

</details>

<details><summary>internal/server/plugin_market_refresh_test.go · go · 5 个命名用例</summary>

[源码入口](../../internal/server/plugin_market_refresh_test.go)

- `TestPluginMarketRefreshFailureVisibleWithoutLosingCache`
- `TestPluginMarketInvalidRefreshDisclosesValidationFailure`
- `TestPluginMarketExternalCatalogMetadata`
- `TestPluginMarketScreenshotRepositoryBoundary`
- `TestPluginMarketRefreshBrowserHost`

环境引用：`AIDE_MARKET_BROWSER_EVIDENCE`, `AIDE_TEST_PUBLIC_MARKET_CATALOG`, `AIDE_TEST_PUBLIC_MARKET_REVISION`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/plugin_market_source_compat_test.go · go · 5 个命名用例</summary>

[源码入口](../../internal/server/plugin_market_source_compat_test.go)

- `TestMarketSourceCompatibilityKeepsReleaseFloor`
- `TestMarketSourceCompatibilityCatalogBrowseAndInstall`
- `TestMarketSourceCompatibilityMetadataBounds`
- `TestMarketSourceOpenAIActualPackage`
- `TestMarketSourceCompatibilityBrowserFixture`

环境引用：`AIDE_OPENAI_MARKET_LISTING`, `AIDE_OPENAI_MARKET_ZIP`, `AIDE_SOURCE_MARKET_BROWSER`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/plugin_market_test.go · go · 8 个命名用例</summary>

[源码入口](../../internal/server/plugin_market_test.go)

- `TestPluginMarketInstallUpdateAndDownload`
- `TestPluginMarketRejectsUnreviewedOrRevokedInstall`
- `TestPluginMarketCatalogIntegrityAndOfflineFallback`
- `TestPluginMarketNumericVersionCompatibility`
- `TestPluginMarketRefreshPinsCatalogAndBoundsCache`
- `TestPluginMarketPublishedPackages`
- `TestPluginMarketInstalledUpgradeHonorsRevocation`
- `TestPluginMarketTerminalWorkbenchCapability`

环境引用：`AIDE_TEST_MARKET_PACKAGES`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/plugin_market_tools_test.go · go · 8 个命名用例</summary>

[源码入口](../../internal/server/plugin_market_tools_test.go)

- `TestMarketBrowseCleanInstallSurfacesAndNoMutation`
- `TestMarketBrowseSearchPagingDetailsAndLocalStatus`
- `TestMarketBrowseRejectsMutationAndInvalidArguments`
- `TestMarketBrowseDenyAndRestrictedPhases`
- `TestMarketBrowseOfflineFallbackAndRefresh`
- `TestMarketBrowseRefreshSharesValidatedRoutingIndex`
- `TestMarketBrowseAgentLoopsWithZeroPlugins`
- `TestMarketBrowseLiveOfficialCatalog`

环境引用：`AIDE_TEST_LIVE_MARKET`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/plugin_plan_test.go · go · 14 个命名用例</summary>

[源码入口](../../internal/server/plugin_plan_test.go)

- `TestPluginPlanValidationAndBudget`
- `TestPluginPlanROS2ParallelAndStableProposalMerge`
- `TestPluginPlanFailureBlocksDependenciesAndReplay`
- `TestPluginPlanRejectsWholeGraphBeforeDispatch`
- `TestPluginPlanAdmissionRechecksSandbox`
- `TestPluginPlanCancellationRecoveryAndNoReplay`
- `TestPluginPlanControlApprovalsAreSerialized`
- `TestPluginPlanModelToolLoopIntegration`
- `TestPluginPlanCentralBudgetPreventsNestedBypass`
- `TestPluginPlanProposalDependenciesAndOutcomeUnknown`
- `TestPluginPlanPersistenceFailurePreventsDispatch`
- `TestPluginPlanCompetingProposalsDoNotOverwrite`
- `TestPluginPlanOutcomeSnapshotCopiesOnlyUncertainty`
- `TestPluginPlanOwnerLookupSurvivesSurfaceRefresh`

</details>

<details><summary>internal/server/plugin_resource_order_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/plugin_resource_order_test.go)

- `TestPluginResourceAdmissionBeforePluginFIFO`

</details>

<details><summary>internal/server/plugin_resources_test.go · go · 8 个命名用例</summary>

[源码入口](../../internal/server/plugin_resources_test.go)

- `TestPluginResourcesAtomicFairnessAndCancellation`
- `TestPluginResourceContractFallbackAndProposalBoundary`
- `TestPluginResourceContractEnforcedAtRealHostResult`
- `TestPluginResourcesDeclaredCallDoesNotLoadUnrelatedModules`
- `TestPluginResourcesCrossPluginWritesAndUnknownEffects`
- `TestPluginResourceWaitRejectsWorkspaceEpochChange`
- `TestPluginProcessCancellationStopsChildWriter`
- `TestPluginResourcesDaemonCancellationStopsChildrenBeforeRelease`

</details>

<details><summary>internal/server/plugin_routing_test.go · go · 10 个命名用例</summary>

[源码入口](../../internal/server/plugin_routing_test.go)

- `TestPluginRoutingMetadataAndBoundaries`
- `TestPluginRoutingPolicyAndDefaultOff`
- `TestPluginRoutingInstallAuthorizationAndDynamicTools`
- `TestPluginRoutingToolReductionAndDeny`
- `TestPluginRoutingInstallRevocationAndScopes`
- `TestPluginRoutingPrivateAndNonmatchNeverInstall`
- `TestPluginRoutingDefaultIndexDownload`
- `TestPluginRoutingModelRoundLoadsDiscoveredTools`
- `TestPluginRoutingCachedSearchDuringNetworkRefresh`
- `TestPluginRoutingBrowser`

环境引用：`AIDE_PLUGIN_ROUTING_BROWSER`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/plugin_source_protected_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/plugin_source_protected_test.go)

- `TestIssue90ExplicitSnapshotRootsRespectHostExclusions`
- `TestIssue90RecursiveSnapshotKeepsOrdinaryAndUserExclusions`

</details>

<details><summary>internal/server/plugin_sources_test.go · go · 6 个命名用例</summary>

[源码入口](../../internal/server/plugin_sources_test.go)

- `TestPluginSourceNativeComparison`
- `TestPluginSourceSnapshotsAndRejection`
- `TestPluginSourceSocketCleanupAndVisibility`
- `TestPluginSourceHostAPI`
- `TestSSHLivePluginSourceSnapshots`
- `TestPluginWorkspaceCacheKeyIsDurableAndScoped`

环境引用：`AIDE_SSH_FIXTURE_A`, `AIDE_SSH_TEST_ISOLATED`, `AIDE_TEST_COMPARE_BUNDLE`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/plugin_terminal_context_test.go · go · 5 个命名用例</summary>

[源码入口](../../internal/server/plugin_terminal_context_test.go)

- `TestTerminalContextReferenceKeysAndBoundaries`
- `TestTerminalContextSSHStorageIsolationWithoutLocalFallback`
- `TestTerminalContextPinnedMasterNeverRetargets`
- `TestTerminalContextCommunicationPolicyAndReferenceKey`
- `TestTerminalRelayContextPrivateStateAndTrustedWorkbench`

</details>

<details><summary>internal/server/plugin_upgrade_browser_fixture_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/plugin_upgrade_browser_fixture_test.go)

- `TestPluginUpgradeBrowserFixture`

环境引用：`AIDE_PLUGIN_UPGRADE_BROWSER_FIXTURE`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/plugin_upgrade_buttons_browser_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/plugin_upgrade_buttons_browser_test.go)

- `TestPluginUpgradeButtonsBrowser`

环境引用：`AIDE_EVIDENCE_DIR`, `AIDE_PLUGIN_BUTTON_BROWSER`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/plugin_upgrade_local_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/plugin_upgrade_local_test.go)

- `TestPluginLocalBinaryUpgradePreservesSettingsAndRecoverableBytes`
- `TestPluginLocalBinaryUpgradeFailuresKeepCurrentVersion`

</details>

<details><summary>internal/server/plugin_upgrade_test.go · go · 14 个命名用例</summary>

[源码入口](../../internal/server/plugin_upgrade_test.go)

- `TestPluginUpgradePreservesStateAndRunsNewResources`
- `TestPluginUpgradePreservesDisabledState`
- `TestPluginUpgradeRejectsWithoutChangingOriginal`
- `TestPluginUpgradeWaitsAndRejectsQueuedOldCode`
- `TestPluginUpgradeDuplicateAndChangedWorkspace`
- `TestPluginUpgradeCancelledWaitKeepsOriginal`
- `TestPluginUpgradeRecoveryRestoresInterruptedSwap`
- `TestPluginUpgradeRecoveryKeepsCommittedSwap`
- `TestPluginUpgradeNetworkPolicy`
- `TestPluginUpgradeRejectsExternalLocalSymlink`
- `TestPluginUpgradeDaemonFailureRollsBackSwappedFiles`
- `TestPluginUpgradeVersionAndDependentGuard`
- `TestPluginUpgradeGitHubFakeIPProxy`
- `TestOfficialPluginDefaultsFollowIndependentMarket`

环境引用：`AIDE_DAEMON`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/plugin_workbench_bridge_test.go · go · 9 个命名用例</summary>

[源码入口](../../internal/server/plugin_workbench_bridge_test.go)

- `TestPluginWorkbenchBridgeOwnToolsReplayAndPolicy`
- `TestPluginWorkbenchBridgeProposalStaysPendingUntilHostApproval`
- `TestPluginWorkbenchBridgeScopeCloseAndPackageRevocation`
- `TestPluginWorkbenchBridgeManifestTools`
- `TestPluginWorkbenchBridgeCancelsDispatchedCallOnScopeChange`
- `TestPluginWorkbenchBridgeRejectsReadOnlySandbox`
- `TestPluginWorkbenchBridgeRejectsProposalFromReadOnlyContract`
- `TestPluginWorkbenchBridgeRenewRotatesAndPreservesScope`
- `TestPluginWorkbenchDetachFinishesOneCallButCannotEscapeRevocation`

</details>

<details><summary>internal/server/plugin_workbench_native_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/plugin_workbench_native_test.go)

- `TestWorkbenchManualSaveConflictAndProposalSeparation`

</details>

<details><summary>internal/server/plugin_workbench_test.go · go · 4 个命名用例</summary>

[源码入口](../../internal/server/plugin_workbench_test.go)

- `TestPluginWorkbenchAuthSandboxAndOneUse`
- `TestPluginWorkbenchRevocationChangeAndBound`
- `TestPluginWorkbenchPackageAndPathValidation`
- `TestPluginWorkbenchFileExtensions`

</details>

<details><summary>internal/server/plugin_workspace_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/plugin_workspace_test.go)

- `TestPluginCurrentWorkspaceFilesPythonAndCWD`
- `TestPluginWorkspaceRootNeverFallsBackForSSH`
- `TestPluginCurrentWorkspaceRejectsLateResult`

</details>

<details><summary>internal/server/plugins_bundle_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/plugins_bundle_test.go)

- `TestPluginBundleInstallsAndRunsPythonTool`
- `TestPluginBundleRejectsTraversalWithoutPartialInstall`
- `TestPluginSettingsCanBeSavedForBundle`

</details>

<details><summary>internal/server/plugins_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/plugins_test.go)

- `TestPluginUploadAndValidation`
- `TestPluginLifecycleAndSurface`
- `TestPluginApplyErrorIsolated`

</details>

<details><summary>internal/server/professional_workflow_bindings_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/professional_workflow_bindings_test.go)

- `TestProfessionalWorkflowBranchContracts`
- `TestProfessionalWorkflowFileBindingAndAutoInput`
- `TestProfessionalWorkflowAutoReadRespectsDisabledTool`

</details>

<details><summary>internal/server/professional_workflow_inference_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/professional_workflow_inference_test.go)

- `TestProfessionalWorkflowInferenceValidation`
- `TestProfessionalWorkflowInferenceParallelAndOptions`
- `TestProfessionalWorkflowAgentToolLoopAndScope`

</details>

<details><summary>internal/server/professional_workflow_runtime_test.go · go · 6 个命名用例</summary>

[源码入口](../../internal/server/professional_workflow_runtime_test.go)

- `TestPWRuntimeMissingAndInvalidSignatureRemainUnavailable`
- `TestPWSealedRuntimeCleanEnvironmentAndOutputBound`
- `TestPWSignedRuntimeIntegrityAndVerifiedImage`
- `TestPWSealedRuntimeCancellation`
- `TestPWSignedAssetsAndExactWorkbenchBytes`
- `TestPWSignedBundleInstallAndUpgradePreserveManifest`

环境引用：`PW_TEST_RUNTIME_DIR`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/professional_workflow_test.go · go · 11 个命名用例</summary>

[源码入口](../../internal/server/professional_workflow_test.go)

- `TestProfessionalWorkflowGraph`
- `TestProfessionalWorkflowStoreAndBindings`
- `TestProfessionalWorkflowRunSnapshotAndCancel`
- `TestProfessionalWorkflowApprovalAndPermission`
- `TestProfessionalWorkflowManualOnlyBridge`
- `TestProfessionalWorkflowModelNode`
- `TestProfessionalWorkflowBuiltinTools`
- `TestProfessionalWorkflowShellKeepsOriginalApproval`
- `TestProfessionalWorkflowExclusiveCapability`
- `TestProfessionalWorkflowControlsRequireCurrentWorkspace`
- `TestProfessionalWorkflowBuiltinHonorsCurrentDisabledTools`

环境引用：`PW_TEST_RUNTIME_DIR`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/profiles_io_bounds_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/profiles_io_bounds_test.go)

- `TestIssue147RoutingPolicyRejectsLinksAndOversizedInput`
- `TestIssue148RejectedProfileSavePreservesMemoryAndDisk`
- `TestIssue147PolicyPriorityFallbackAndExactLimit`

</details>

<details><summary>internal/server/profiles_io_unix_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/profiles_io_unix_test.go)

- `TestIssue147PolicyFIFODoesNotBlockAndUsesRegularFallback`

</details>

<details><summary>internal/server/profiles_test.go · go · 7 个命名用例</summary>

[源码入口](../../internal/server/profiles_test.go)

- `TestSystemProfilesImmutability`
- `TestProfileValidation`
- `TestUserProfilePersistence`
- `TestAutoRoutingPolicy`
- `TestProfileParamsReachProvider`
- `TestTaskRecordsStrategyAndProfile`
- `TestVersionFromVersionFile`

</details>

<details><summary>internal/server/project_briefing_test.go · go · 4 个命名用例</summary>

[源码入口](../../internal/server/project_briefing_test.go)

- `TestProjectMessagesScopeDateAndProvenance`
- `TestProjectMessagesBridgeDeclarationReplayAndValidation`
- `TestProjectMessagesProviderMetadataNotSent`
- `TestProjectMessageLegacyTimeCompatibility`

</details>

<details><summary>internal/server/project_cache_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/project_cache_test.go)

- `TestProjectCacheIsolation`
- `TestLocalCacheCommandAndMemoryAgree`

</details>

<details><summary>internal/server/project_reference_bounds_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/project_reference_bounds_test.go)

- `TestIssue87OnlyExplicitProjectReferencesSelectRuns`
- `TestIssue87ReferenceMarkersRequireWholeIdentity`

</details>

<details><summary>internal/server/provider_balance_test.go · go · 8 个命名用例</summary>

[源码入口](../../internal/server/provider_balance_test.go)

- `TestProviderBalanceNoPluginNeverRequestsNetwork`
- `TestProviderBalanceSelectedKeyAndSanitizedAccount`
- `TestProviderBillingCredentialsSeparateVaultAndClear`
- `TestProviderBillingKeySnapshotSurvivesGlobalSelectionChange`
- `TestProviderBillingIsolatedAssistantDoesNotInheritGlobalKey`
- `TestProviderBillingManualPriceRequiresExplicitPair`
- `TestProviderBillingCredentialsRejectLockedAndUnauthorized`
- `TestProviderBillingCredentialsAreBoundToSelectedAPIKey`

</details>

<details><summary>internal/server/provider_error_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/provider_error_test.go)

- `TestCompleteIncludesSafeProviderErrorDetail`

</details>

<details><summary>internal/server/provider_image_test.go · go · 4 个命名用例</summary>

[源码入口](../../internal/server/provider_image_test.go)

- `TestQwenImageWireProtocol`
- `TestQwenImageNativeProtocol`
- `TestQwenImageErrorsDoNotRetryOrLeak`
- `TestQwenImageChatSingleRequest`

</details>

<details><summary>internal/server/provider_media_generation_test.go · go · 6 个命名用例</summary>

[源码入口](../../internal/server/provider_media_generation_test.go)

- `TestMediaGenerationIntent`
- `TestNativeMediaProtocolsAndResume`
- `TestNativeMediaCancellationAndPersistenceFailure`
- `TestAutoMediaRouteOutputWeights`
- `TestTTSToolsPreserveAssistantSettings`
- `TestGeneratedMediaRawMIMEAndRange`

</details>

<details><summary>internal/server/provider_openai_test.go · go · 6 个命名用例</summary>

[源码入口](../../internal/server/provider_openai_test.go)

- `TestOpenAIManualReasoningParameters`
- `TestOpenAIWireOverridesRouterButHonorsCapabilities`
- `TestOpenAIEndpointIsolation`
- `TestOpenAIReasoningOffModelSupport`
- `TestOpenAISessionReasoningSelection`
- `TestOpenAIReasoningOptionsAndModelSwitch`

</details>

<details><summary>internal/server/provider_quota_package_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/provider_quota_package_test.go)

- `TestPluginQuotaActualPackage`
- `TestPluginQuotaBrowserFixture`

环境引用：`AIDE_OPENAI_PACKAGE`, `AIDE_QUOTA_BROWSER`, `AIDE_QUOTA_PLUGIN_PACKAGE`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/provider_quota_test.go · go · 5 个命名用例</summary>

[源码入口](../../internal/server/provider_quota_test.go)

- `TestPluginQuotaSelectedOriginKeyAndAllowlist`
- `TestPluginQuotaLocalOriginAndPathBounds`
- `TestPluginQuotaInflightDisableRejectsResult`
- `TestPluginQuotaUnavailableAndPricingOnly`
- `TestPluginQuotaLegacyMalformedAndRedirect`

</details>

<details><summary>internal/server/provider_qwen_media_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/provider_qwen_media_test.go)

- `TestQwenMediaSelectionGate`
- `TestQwenWorkspaceTariffIsolation`
- `TestQwenGeneratedAssetOrigins`

</details>

<details><summary>internal/server/provider_qwen_test.go · go · 4 个命名用例</summary>

[源码入口](../../internal/server/provider_qwen_test.go)

- `TestQwenMainstreamWire`
- `TestQwenRealtimeTextToolBridge`
- `TestQwenRealtimeRequiresWorkspace`
- `TestQwenRealtimeCancellation`

</details>

<details><summary>internal/server/provider_responses_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/provider_responses_test.go)

- `TestOpenAIResponsesToolRoundTrip`
- `TestOpenAIResponsesTruncatedStream`

</details>

<details><summary>internal/server/provider_stream_test.go · go · 6 个命名用例</summary>

[源码入口](../../internal/server/provider_stream_test.go)

- `TestExplicitModelCapabilitiesControlProviderFeatures`
- `TestCompleteStreamTextDeltas`
- `TestCompleteStreamToolCallAssembly`
- `TestCompleteStreamNonSSEFallback`
- `TestCompleteStreamHTTPError`
- `TestCompleteStreamStopsAfterIdleTimeout`

</details>

<details><summary>internal/server/provider_support_test.go · go · 16 个命名用例</summary>

[源码入口](../../internal/server/provider_support_test.go)

- `TestProviderSupportActualPackagesEnabledDisabledRemoved`
- `TestProviderSupportExactHostPathAndModel`
- `TestProviderSupportOpenAIUsdIsMatchedAndAggregatedSeparately`
- `TestManualTokenPricingCurrencyDefaultsToCNYAndAcceptsUSD`
- `TestProviderSupportMalformedEscapingAndAmbiguousCatalogs`
- `TestProviderSupportCatalogValidationRejectsInvalidPricesTiersAndBalanceHosts`
- `TestProviderSupportDeepSeekPeakCacheAndCalendarEstimate`
- `TestProviderSupportAliWholeRequestTiersRegionAndReasoning`
- `TestProviderSupportScopedPriceWriteAndPersistedKeyIdentity`
- `TestProviderSupportBalanceNormalizationAndSigning`
- `TestProviderSupportKeyOverridesInheritProviderModelAndAllKeyView`
- `TestProviderSupportBearerBalanceCannotCrossDeclaredProviderHosts`
- `TestProviderSupportQuoteSourceSafeDuringRegistryReplacement`
- `TestProviderSupportKeylessLocalProviderRetainsManualPricing`
- `TestProviderSupportZeroReasoningCounterDoesNotProveNonThinkingMode`
- `TestProviderSupportPluginPriorityAndManualFallback`

环境引用：`AIDE_SUPPORTER_ZIP_DIR`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/queue_admission_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/queue_admission_test.go)

- `TestIssue123RejectedQueuedSteerPreservesPromptAndDisplay`

</details>

<details><summary>internal/server/queue_steer_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/queue_steer_test.go)

- `TestToolLoopSteerAndQueue`
- `TestStartTaskSteerAndQueueBranch`
- `TestQueueUpdateActions`

</details>

<details><summary>internal/server/qwen_tts_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/qwen_tts_test.go)

- `TestQwenTTSSettings`
- `TestQwenCatalogReadOnly`

</details>

<details><summary>internal/server/rca_reports_test.go · go · 5 个命名用例</summary>

[源码入口](../../internal/server/rca_reports_test.go)

- `TestRCAValidWorkflowPhase`
- `TestRCANumberingAndIndex`
- `TestRCADiagramExistence`
- `TestRCARequiredSections`
- `TestExtractRCADiagramPath`

</details>

<details><summary>internal/server/reasoning_settings_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/reasoning_settings_test.go)

- `TestReasoningDefaultWithoutModelAndReload`
- `TestReasoningDefaultPreservesSessionChoice`

</details>

<details><summary>internal/server/release_browser_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/release_browser_test.go)

- `TestReleaseBrowserMatrix`

环境引用：`AIDE_EVIDENCE_DIR`, `AIDE_RELEASE_BROWSER`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/release_public_dependencies_test.go · go · 0 个命名用例</summary>

[源码入口](../../internal/server/release_public_dependencies_test.go)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>internal/server/release_public_plugins_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/release_public_plugins_test.go)

- `TestReleasePublicPackageFunctions`

环境引用：`AIDE_PUBLIC_PLUGIN_EVIDENCE_DIR`, `AIDE_RELEASE_PUBLIC_MARKET`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/remediation_test.go · go · 21 个命名用例</summary>

[源码入口](../../internal/server/remediation_test.go)

- `TestRemediationAutoCompactNotBlockedByHeldMutex`
- `TestContextOverflowCompactsHistoryBeforeRetry`
- `TestAutoCompactionPublishesObservableLifecycle`
- `TestAutoCompactionCancellationPublishesTerminalAndPreservesHistory`
- `TestProviderCompleteHonorsRequestCancellation`
- `TestManualCompactionPublishesLiveProgressAndTokenEstimates`
- `TestCompactionRetainedHistoryStaysWithinBudget`
- `TestStartTaskCompactsOverBudgetHistoryBeforeProviderCall`
- `TestRemediationConcurrentCompactionNoPanicNoDeadlock`
- `TestRemediationOldProposalRejectedOnNewWorkspace`
- `TestRemediationCommandFollowsCurrentWorkspace`
- `TestRemediationVirtualRootTraversalRejected`
- `TestRemediationCompactionChainRetainsPreviousSummary`
- `TestRemediationWriteToolPreservesWhitespace`
- `TestRemediationSessionAPIStableDuringRun`
- `TestR08ZeroPricingIsRealZero`
- `TestR08PriceSnapshotStable`
- `TestR08LegacyMigration`
- `TestCompactionSummaryUsesBoundedRequestsAndCarriesPriorSummary`
- `TestMalformedCompactionSummaryAbortsWithoutChangingSource`
- `TestCompactionSnapshotDetectsToolCallChanges`

</details>

<details><summary>internal/server/reminder_schedule_browser_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/reminder_schedule_browser_test.go)

- `TestReminderScheduleBrowser`

环境引用：`AIDE_REMINDER_SCHEDULE_BROWSER`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/reminder_schedule_test.go · go · 7 个命名用例</summary>

[源码入口](../../internal/server/reminder_schedule_test.go)

- `TestReminderScheduleValidationAndPermissions`
- `TestReminderScheduleDueBusyAndAtMostOnce`
- `TestReminderScheduleWorkspaceCancelAndApproval`
- `TestReminderScheduleRecoveryAndPersistenceFailure`
- `TestReminderScheduleLockedXiaomiAndResult`
- `TestReminderScheduleRealRunAndXiaomi`
- `TestReminderScheduleDelegationKeepsWorkspace`

</details>

<details><summary>internal/server/reminders_test.go · go · 4 个命名用例</summary>

[源码入口](../../internal/server/reminders_test.go)

- `TestXiaomiReminderToolSchemasOmitNullRequired`
- `TestReminderAPICRUDSearchAndScopes`
- `TestReminderAidePermissionsAndXiaomiTools`
- `TestDueRemindersPopupAndXiaomiDispatchAreIdempotent`

</details>

<details><summary>internal/server/remote_group_cleanup_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/remote_group_cleanup_test.go)

- `TestRemoteCommandWrappedInOwnPG`
- `TestRemoteCancelKillsRemoteGroup`
- `TestRemoteTimeoutKillsRemoteGroup`

</details>

<details><summary>internal/server/remote_shell_cancel_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/remote_shell_cancel_test.go)

- `TestRemoteShellCommandCancellable`

</details>

<details><summary>internal/server/research_log_scope_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/research_log_scope_test.go)

- `TestResearchCoverageClaimScope`
- `TestSystemLogQuotedCredentials`

</details>

<details><summary>internal/server/research_value_match_test.go · go · 4 个命名用例</summary>

[源码入口](../../internal/server/research_value_match_test.go)

- `TestIssue158FindingRequiresWholeValueInOriginalResult`
- `TestIssue158NarrowQuoteCanFindLaterWholeOccurrence`
- `TestIssue158RejectedRevisionKeepsPriorFinding`
- `TestIssue158PeriodicQuoteKeepsOriginalBoundaries`

</details>

<details><summary>internal/server/retry_context_test.go · go · 4 个命名用例</summary>

[源码入口](../../internal/server/retry_context_test.go)

- `TestRetryOlderTaskExcludesLaterConversation`
- `TestRetryConversationLegacyOccurrences`
- `TestRetryConversationCompactedOriginalSnapshot`
- `TestRetryMissingHistoryRejectsWithoutAddingTask`

</details>

<details><summary>internal/server/run_controls_test.go · go · 7 个命名用例</summary>

[源码入口](../../internal/server/run_controls_test.go)

- `TestStopEndpointIsolationAndDurableIntent`
- `TestStopEndpointLateTerminalIsNoop`
- `TestStopIntentSurvivesRestart`
- `TestRunEventsReconnectWhileAwaitingAnswer`
- `TestStopEndpointSaveFailureKeepsPauseIntent`
- `TestStopIntentWinsLateProviderSuccess`
- `TestStopPendingRejectsAnswerAndSteer`

</details>

<details><summary>internal/server/runtime_shutdown_test.go · go · 4 个命名用例</summary>

[源码入口](../../internal/server/runtime_shutdown_test.go)

- `TestRuntimeShutdownFlushesPrivacyEventAndCancelsWork`
- `TestRuntimeShutdownPrioritizesPrivacyOverRefreshBacklog`
- `TestSourceInstallationDisablesABEvenForReleaseBinary`
- `TestReleaseInstallationRetainsABCapability`

</details>

<details><summary>internal/server/sandbox_policy_test.go · go · 5 个命名用例</summary>

[源码入口](../../internal/server/sandbox_policy_test.go)

- `TestSandboxCommandPanelEnforcement`
- `TestSandboxReadonlyPrecedesApprovalAndProposalApply`
- `TestSandboxModesAndRemoteBoundary`
- `TestSandboxReadonlyWorkspaceTools`
- `TestSandboxConfigurationPersistsAndAffectsNextCommand`

</details>

<details><summary>internal/server/secret_vault_test.go · go · 10 个命名用例</summary>

[源码入口](../../internal/server/secret_vault_test.go)

- `TestVaultPutGet`
- `TestVaultListNoPlaintext`
- `TestVaultDelete`
- `TestVaultReWrap`
- `TestVaultFilePermission`
- `TestExportImportEncrypted`
- `TestVaultAutoUnlockNoAccountPassword`
- `TestPathValidationReject`
- `TestFingerprint`
- `TestTempDirCleanup`

</details>

<details><summary>internal/server/semantic_search_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/semantic_search_test.go)

- `TestSemanticSearchReferenceSource`

</details>

<details><summary>internal/server/server_test.go · go · 10 个命名用例</summary>

[源码入口](../../internal/server/server_test.go)

- `TestAppCloseWaitsForTrackedBackgroundWork`
- `TestAuthenticationAndOrigin`
- `TestFileBoundariesAndConflicts`
- `TestSettingsNeverReturnKey`
- `TestWorkflowApprovalConflictAndPersistence`
- `TestRejectUnattachedExistingFile`
- `TestProviderErrorsAndCancellation`
- `TestCommandExecutionAndExitCode`
- `TestRestartMarksInterrupted`
- `TestChatHistoryAndCancelEndpoint`

</details>

<details><summary>internal/server/session_bucket_migration_test.go · go · 7 个命名用例</summary>

[源码入口](../../internal/server/session_bucket_migration_test.go)

- `TestSavePicksCanonicalBucket`
- `TestArchiveMoveMovesFileBetweenBuckets`
- `TestMigrationLegacyLayoutRelocates`
- `TestSessionBucketIdempotent`
- `TestSessionBucketRollback`
- `TestMigrationCorruptSessionUntouched`
- `TestRoundTripAllSessionsSurviveRestart`

</details>

<details><summary>internal/server/session_manage_test.go · go · 5 个命名用例</summary>

[源码入口](../../internal/server/session_manage_test.go)

- `TestSessionPinArchiveListAndPersistence`
- `TestSessionDeleteRemovesFileAndCancelsRun`
- `TestExportSessions`
- `TestSessionOrderByActivity`
- `TestSessionCheckedFlag`

</details>

<details><summary>internal/server/session_model_selection_test.go · go · 6 个命名用例</summary>

[源码入口](../../internal/server/session_model_selection_test.go)

- `TestSessionModelSelectionAPI`
- `TestSessionModelSelectionRoutingAndSnapshots`
- `TestSessionModelSelectionLegacyAndVision`
- `TestSessionModelSelectionLateConfiguration`
- `TestSessionModelSelectionLegacyProviderRoute`
- `TestSessionModelSelectionSteerKeepsOriginalTask`

</details>

<details><summary>internal/server/session_number_test.go · go · 17 个命名用例</summary>

[源码入口](../../internal/server/session_number_test.go)

- `TestSessionNumberUnique`
- `TestSessionNumberNotReused`
- `TestSessionNumberSurvivesPartialSettingsUpdates`
- `TestSessionNumberDuplicatesAreRepairedOnStartup`
- `TestAssistantSessionIdempotent`
- `TestAssistantSessionPinned`
- `TestAssistantSessionTitleSync`
- `TestAssistantSessionPasswordGate`
- `TestSearchSessions`
- `TestGetSessionByNumber`
- `TestGetSessionByID`
- `TestFollowSession`
- `TestPushToSession`
- `TestCrossSessionToolsOnlyAssistant`
- `TestSessionNumberCanonicalPersistence`
- `TestSessionNumberCanonicalRepairPersistence`
- `TestSessionNumberLegacyCounterPreservesCanonicalSettings`

</details>

<details><summary>internal/server/session_running_history_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/session_running_history_test.go)

- `TestSessionHistoryRunningMetadataAcrossPages`

</details>

<details><summary>internal/server/settings_persona_persist_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/settings_persona_persist_test.go)

- `TestSettingsPartialPutPreservesFields`
- `TestSettingsPartialPutPreservesVoiceDispatch`

</details>

<details><summary>internal/server/sftp_attribute_bounds_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/sftp_attribute_bounds_test.go)

- `TestIssue163SFTPCountCannotAllocateWithoutPayload`
- `TestIssue163SFTPExtendedAttributeBoundaries`

</details>

<details><summary>internal/server/sftp_bounds_test.go · go · 4 个命名用例</summary>

[源码入口](../../internal/server/sftp_bounds_test.go)

- `TestIssue79SFTPReadBoundsAndBinaryControl`
- `TestIssue79SFTPReadLimitCatchesStaleOrFalseStat`
- `TestIssue79BatchOutputProcess`
- `TestIssue79SFTPBatchOutputBoundedDuringCapture`

环境引用：`AIDE_SFTP_OUTPUT_FINISHED`, `AIDE_SFTP_OUTPUT_HELPER`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/sftp_cancel_boundary_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/sftp_cancel_boundary_test.go)

- `TestIssue617VisitorCancellationStopsBeforeNextReadDir`

</details>

<details><summary>internal/server/sftp_name_bounds_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/sftp_name_bounds_test.go)

- `TestIssue164MalformedNamesReturnErrorsWithoutPanic`
- `TestIssue164ValidNamesKeepAttributesAndVisitorSemantics`
- `TestIssue164RoutingIDsAlreadyFailClosed`

</details>

<details><summary>internal/server/sftp_packet_fixture_test.go · go · 0 个命名用例</summary>

[源码入口](../../internal/server/sftp_packet_fixture_test.go)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>internal/server/shell_cancel_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/shell_cancel_test.go)

- `TestExecShellCommandCancellable`
- `TestExecShellCommandStillHonorsTimeout`

</details>

<details><summary>internal/server/shell_readonly_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/shell_readonly_test.go)

- `TestBasicReadOnlyShellClassification`
- `TestReadOnlyShellCommandLengthBoundsWithoutCommandCountCap`
- `TestBasicReadOnlyShellExecutesWithoutReviewer`

</details>

<details><summary>internal/server/skill_library_test.go · go · 8 个命名用例</summary>

[源码入口](../../internal/server/skill_library_test.go)

- `TestSkillLibrarySearchCachePrepareConfirmReadAndLocks`
- `TestSkillLibraryRolesReadonlyDisabledAndCancellation`
- `TestSkillLibraryProposalRevocationTamperAndPluginChange`
- `TestSkillLibraryRejectsUntrustedDownloadsAndVirtualTraversal`
- `TestSkillMetadataAndRanking`
- `TestSkillLibraryLivePublicCatalog`
- `TestSkillLibraryBundleImportAndNativeOwner`
- `TestSkillLibraryCancelsInflightAndRechecksGeneration`

环境引用：`AIDE_TEST_LIVE_SKILLS`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/source_ai_visibility_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/source_ai_visibility_test.go)

- `TestSourceAIVisibilityWorkspaceScope`
- `TestBuiltinSourceAIVisibilitySurvivesSourceEdit`

</details>

<details><summary>internal/server/source_browse_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/source_browse_test.go)

- `TestSourceBrowseDraftDoesNotSaveSource`
- `TestSourceBrowseRejectsInvalidInput`

</details>

<details><summary>internal/server/source_curl_bounds_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/source_curl_bounds_test.go)

- `TestIssue76CurlOutputProcess`
- `TestIssue76CurlOutputIsBoundedDuringCapture`

环境引用：`AIDE_CURL_BOUNDS_HELPER`, `AIDE_CURL_COMPLETED`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/source_file_actions_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/source_file_actions_test.go)

- `TestSourceFileActionsLiveSFTP`
- `TestWritableSourceFileActions`
- `TestSourceFileActionsRejectReadonlyAndInvalidTargets`

环境引用：`AIDE_SOURCE_ACTION_FIXTURE_HOST`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/source_relay_browser_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/source_relay_browser_test.go)

- `TestReferenceRelayBrowserFixture`

环境引用：`AIDE_RELAY_BROWSER_FIXTURE`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/source_relay_test.go · go · 7 个命名用例</summary>

[源码入口](../../internal/server/source_relay_test.go)

- `TestReferenceRelayDownloadedAgent`
- `TestReferenceRelayRemoteReadOnly`
- `TestReferenceRelayStreamingAndRevocation`
- `TestReferenceRelayDurableTransfer`
- `TestReferenceRelayProtocolBounds`
- `TestReferenceRelayVaultAndProposalBinding`
- `TestReferenceRelayUntrustedPayloadAndCancellation`

</details>

<details><summary>internal/server/source_root_open_test.go · go · 5 个命名用例</summary>

[源码入口](../../internal/server/source_root_open_test.go)

- `TestLocalRootOpenPendingUsesInitiatingDeadline`
- `TestLocalRootOpenPendingUsesShortCallerDeadline`
- `TestLocalRootOpenTimeoutDeduplicatesAndClosesLateHandle`
- `TestLocalRootOpenAdmissionAndShutdownAreBounded`
- `TestLocalRootOpenConcurrentReadersKeepIndependentHandles`

</details>

<details><summary>internal/server/source_secrets_vault_test.go · go · 9 个命名用例</summary>

[源码入口](../../internal/server/source_secrets_vault_test.go)

- `TestSourceSecretsMigrationPlaintextToVault`
- `TestSourceSecretsFallbackWhenVaultLocked`
- `TestSourceSecretsMigrationIdempotent`
- `TestSourceCredentialStoreAndClear`
- `TestSourceCredentialSweepRemoved`
- `TestUpdateSourcesPartialVaultCredentials`
- `TestUpdateSourcesEmptyVaultCredentialPatchIsNoop`
- `TestUpdateSourcesUnreadablePartialVaultPatchIsAtomic`
- `TestUpdateSourcesClearLockedVaultCredentials`

</details>

<details><summary>internal/server/source_sftp_relay_test.go · go · 4 个命名用例</summary>

[源码入口](../../internal/server/source_sftp_relay_test.go)

- `TestSFTPRelayActualSSH`
- `TestSFTPRelayBrowserFixture`
- `TestSFTPRelayVaultPatch`
- `TestSFTPRelayRejectMalformedExistingCredentials`

环境引用：`AIDE_SFTP_RELAY_BROWSER_FIXTURE`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/sources_live_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/sources_live_test.go)

- `TestSourcesLiveProtocols`

环境引用：`AIDE_SOURCE_FIXTURE_HOST`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/sources_test.go · go · 7 个命名用例</summary>

[源码入口](../../internal/server/sources_test.go)

- `TestSourcesRegistryAndBrowsing`
- `TestSystemDocsAutoMount`
- `TestCurlSourceWithMock`
- `TestMCPReferenceDiscoveryAndReadOnlyCall`
- `TestLinkSourceUsesExactURL`
- `TestFTPSourceNestedListing`
- `TestAIReferenceAccess`

</details>

<details><summary>internal/server/ssh_apply_test.go · go · 6 个命名用例</summary>

[源码入口](../../internal/server/ssh_apply_test.go)

- `TestSSHApplyCompletesAndPersists`
- `TestSSHApplyRechecksStateAfterRead`
- `TestSSHApplyRecordsWriteBeforeStopping`
- `TestSSHApplyNewFileRequiresConfirmedAbsence`
- `TestSSHApplyWaitsForFilesWithoutHoldingAppLock`
- `TestSSHApplySerializesApprovals`

</details>

<details><summary>internal/server/ssh_approval_live_test.go · go · 0 个命名用例</summary>

[源码入口](../../internal/server/ssh_approval_live_test.go)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>internal/server/ssh_continuity_live_test.go · go · 0 个命名用例</summary>

[源码入口](../../internal/server/ssh_continuity_live_test.go)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>internal/server/ssh_filename_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/ssh_filename_test.go)

- `TestSFTPListDecodesChineseFilenames`
- `TestSFTPListMarksSymlinksWithoutTreatingThemAsDirectories`

</details>

<details><summary>internal/server/ssh_keyutil_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/ssh_keyutil_test.go)

- `TestSshKeygenPassphraseViaAskpass`
- `TestSshKeygenArgvLeakage`

</details>

<details><summary>internal/server/ssh_live_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/ssh_live_test.go)

- `TestSSHFailedControlCheckPreservesLiveSocket`
- `TestSSHLiveWorkspaceAndIndependentSource`

环境引用：`AIDE_SSH_FIXTURE_A`, `AIDE_SSH_FIXTURE_B`, `AIDE_SSH_TEST_ISOLATED`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/ssh_passphrase_pipe_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/ssh_passphrase_pipe_test.go)

- `TestIssue103LargePassphraseNeverBlocksBeforeChildStart`
- `TestIssue103AskpassHelperProcess`

环境引用：`AIDE_ISSUE103_MODE`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/ssh_session_reliability_test.go · go · 19 个命名用例</summary>

[源码入口](../../internal/server/ssh_session_reliability_test.go)

- `TestSSHSessionLifecycleConcurrentConnect`
- `TestSSHSessionLifecycleCanceledWaiter`
- `TestSSHSessionTemporaryAskpassRemoved`
- `TestSSHSourceSessionMustVerifyMaster`
- `TestSSHSessionConnectHonorsCallerDeadline`
- `TestSFTPDirectoryReconnectIsReadOnly`
- `TestSSHReconnectDoesNotCloseNewerSocket`
- `TestSSHRemoteCommandStatusAndCleanup`
- `TestSSHRemoteExecKeepsExitCode`
- `TestSSHRemoteExecWaitsForForkedSession`
- `TestSSHRemoteCleanupExecutesAndStopsOwnedGroup`
- `TestSSHRemoteCleanupFailureIsVisible`
- `TestSFTPFailureWithZeroExit`
- `TestSFTPDefaultPortMatchesSSH`
- `TestSSHGenerationRejectsQueuedOldConfig`
- `TestSSHSourceRejectsDeletedOrChangedConfig`
- `TestSFTPGenerationBoundReadWriteRejectNewConfig`
- `TestSFTPBoundTransferKeepsMasterUntilFinished`
- `TestSSHSessionInvalidatedMasterAfterKillTimeout`

</details>

<details><summary>internal/server/staged_output_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/staged_output_test.go)

- `TestStagedOutputPersistsBeforeNextRound`
- `TestStagedOutputFinalAndResumeAppend`
- `TestStagedOutputExcludesInternalAndFailsClosed`

</details>

<details><summary>internal/server/static_audit_benchmark_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/static_audit_benchmark_test.go)

- `TestStaticAuditCoverageRejectsClaimsAndMismatchedContent`
- `TestStaticAuditLiveLuna`

环境引用：`AIDE_STATIC_AUDIT_LIVE_DATA`, `AIDE_STATIC_AUDIT_ONLY`, `AIDE_STATIC_AUDIT_PROOF`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/static_audit_fixture_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/static_audit_fixture_test.go)

- `TestStaticAuditFixture`
- `TestStaticAuditSeedInvariants`

环境引用：`AIDE_STATIC_AUDIT_PREPARE`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/step_preview_test.go · go · 5 个命名用例</summary>

[源码入口](../../internal/server/step_preview_test.go)

- `TestStepLargeStreamingAndRange`
- `TestStepStreamingSourceAndIdentity`
- `TestStepStreamingLimitsAndPathSafety`
- `TestStepRemoteConcurrencyBound`
- `TestStepNamedPipeRejectedBeforeOpen`

</details>

<details><summary>internal/server/stream_broker_retention_test.go · go · 5 个命名用例</summary>

[源码入口](../../internal/server/stream_broker_retention_test.go)

- `TestIssue145BrokerGlobalRetention`
- `TestIssue145BrokerActiveCapacityAndLRU`
- `TestIssue145DeleteFencesConcurrentCallbacks`
- `TestIssue145ResetDropsLiveOutput`
- `TestIssue145SessionDeleteEvictsOutput`

</details>

<details><summary>internal/server/stream_broker_test.go · go · 8 个命名用例</summary>

[源码入口](../../internal/server/stream_broker_test.go)

- `TestStreamBrokerPublish`
- `TestStreamBrokerGetLiveOutput`
- `TestStreamBrokerInterrupted`
- `TestStreamBrokerDone`
- `TestStreamBrokerMultipleSessions`
- `TestStreamBrokerResetOnNewRun`
- `TestStreamBrokerCap`
- `TestLiveRunHelpersWired`

</details>

<details><summary>internal/server/stream_events_test.go · go · 7 个命名用例</summary>

[源码入口](../../internal/server/stream_events_test.go)

- `TestRunEventsLiveStreamingChat`
- `TestRunEventsFinishedTaskCloses`
- `TestRunEventsAuthScoping`
- `TestRunEventsCancelMidStream`
- `TestStreamHubCloseIsTerminal`
- `TestCompleteStreamRetryWithoutStreamOptions`
- `TestCompleteStreamCancelMidStream`

</details>

<details><summary>internal/server/system_log_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/system_log_test.go)

- `TestSystemLogSnapshotFiltersInclusiveTimeRange`
- `TestSystemLogsHandlerRejectsInvalidTimeRange`
- `TestSystemLogsHandlerAppliesTimeFiltersToJSONAndDownload`

</details>

<details><summary>internal/server/task_delivery_provenance_test.go · go · 9 个命名用例</summary>

[源码入口](../../internal/server/task_delivery_provenance_test.go)

- `TestIssue146UnchangedArtifactCannotCompleteTask`
- `TestIssue146PartialDirectoryCoverageDoesNotInventFreshness`
- `TestIssue146BaselineSaveFailureStopsBeforeProvider`
- `TestIssue146CompletedExecutionDropsBaseline`
- `TestIssue146NewAndChangedArtifactsComplete`
- `TestIssue146BaselinePersistsAndRejectsAliases`
- `TestIssue146NativeResumeAndAppliedWritesAreEvidence`
- `TestIssue146CaseAliasesAreNotNewPaths`
- `TestIssue146LargeNewAndSizeChangedArtifacts`

</details>

<details><summary>internal/server/task_outcome_test.go · go · 7 个命名用例</summary>

[源码入口](../../internal/server/task_outcome_test.go)

- `TestTaskOutcomePreservesEvidenceBoundaries`
- `TestOutcomeShellWriteTargetsRemainUnverifiedHints`
- `TestTaskOutcomeEvidenceDigestAndOwnership`
- `TestTaskOutcomeSnapshotIsDetached`
- `TestTaskOutcomeSystemReceipt`
- `TestTaskOutcomePaginationSnapshot`
- `TestTaskOutcomeInputsAndOutputs`

</details>

<details><summary>internal/server/task_recovery_test.go · go · 6 个命名用例</summary>

[源码入口](../../internal/server/task_recovery_test.go)

- `TestTaskRecoveryPreviewAndGuardedResume`
- `TestTaskRecoveryOrdinaryCheckpointKeepsLegacyWorkspaceBehavior`
- `TestTaskRecoveryContinuesNewGraphNeverReplaysOld`
- `TestTaskRecoveryGenericCallsAndCurrentPolicy`
- `TestTaskRecoveryNewIDsCannotRepeatUncertainEffects`
- `TestTaskRecoverySaveFailureAndConcurrentDecision`

</details>

<details><summary>internal/server/task_release_receipts_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/task_release_receipts_test.go)

- `TestOutcomeReleaseAcceptsItsReadSnapshot`

</details>

<details><summary>internal/server/task_run_budget_test.go · go · 4 个命名用例</summary>

[源码入口](../../internal/server/task_run_budget_test.go)

- `TestTaskRunBudgetPausesDuringHumanWait`
- `TestTaskRunBudgetNestedPauseWaitsForEveryResume`
- `TestTaskRunBudgetParentCancellationSurvivesPause`
- `TestAwaitUserAnswerPausesTaskRunBudget`

</details>

<details><summary>internal/server/task_ssh_pin_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/task_ssh_pin_test.go)

- `TestIssue82SocketPinSurvivesReplacementAndCleansAlias`
- `TestIssue82CancellationUsesPinnedOriginalSSHSession`

</details>

<details><summary>internal/server/task_workspace_guard_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/task_workspace_guard_test.go)

- `TestIssue82StaleRemoteTaskCannotDispatchOrApprove`
- `TestIssue82MatchingRemoteTaskSkillAndApproval`

</details>

<details><summary>internal/server/terminal_pin_lifecycle_test.go · go · 4 个命名用例</summary>

[源码入口](../../internal/server/terminal_pin_lifecycle_test.go)

- `TestIssue92ReplacedMasterPinsReapOnlyDeadListeners`
- `TestIssue92AppOwnsPinsAndShutdownPreservesOtherOwners`
- `TestIssue92ReaperPreservesUnknownAndReplacedPaths`
- `TestIssue92ConcurrentPinShutdownHasNoLateLinks`

</details>

<details><summary>internal/server/terminal_workbench_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/terminal_workbench_test.go)

- `TestTerminalWorkbenchBundleInstallsStartsAndRetainsLegacyTools`

环境引用：`AIDE_TERMINAL_BUNDLE_PATH`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/tls_test.go · go · 7 个命名用例</summary>

[源码入口](../../internal/server/tls_test.go)

- `TestAutoGenerateCert`
- `TestTLSConfig`
- `TestHTTPRedirect`
- `TestSplitProtoClassification`
- `TestMigrateLegacyTLSDir`
- `TestHSTSNotOnLocalhost`
- `TestProtoListenerConcurrentClose`

</details>

<details><summary>internal/server/token_snapshot_failure_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/token_snapshot_failure_test.go)

- `TestIssue107SnapshotFailureIsVisibleAndNextSaveIncludesPendingUsage`

</details>

<details><summary>internal/server/tool_calls_test.go · go · 7 个命名用例</summary>

[源码入口](../../internal/server/tool_calls_test.go)

- `TestToolCallsAggregateMainAndSub`
- `TestToolCallsFilterWho`
- `TestToolCallsFilterChildSessionId`
- `TestToolCallsArchivedChildStillQueriable`
- `TestToolCallsLegacyWhoDefaultsMain`
- `TestToolCallsNotFound`
- `TestToolCallsExportIncludesSubCalls`

</details>

<details><summary>internal/server/tools_test.go · go · 8 个命名用例</summary>

[源码入口](../../internal/server/tools_test.go)

- `TestToolLoopReadFile`
- `TestToolLoopWriteProposalAndShellExec`
- `TestPluginToolExecution`
- `TestToolLoopWriteUnattachedRejected`
- `TestTokenStats`
- `TestSearchAndCompaction`
- `TestShellBlocked`
- `TestReadOnlyAllowed`

</details>

<details><summary>internal/server/transfer_jobs_test.go · go · 4 个命名用例</summary>

[源码入口](../../internal/server/transfer_jobs_test.go)

- `TestLargeTransferUploadConcurrentJournalRestart`
- `TestLargeTransferUploadCorruptionRepairAndCancel`
- `TestLargeTransferCopyResumeMoveAndConflict`
- `TestLargeTransferDownloadRangesAndBoundaries`

</details>

<details><summary>internal/server/transfer_manifest_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/transfer_manifest_test.go)

- `TestIssue71RecursiveManifestAdmissionBounds`
- `TestIssue71DirectoryEnumerationStopsAndCancels`
- `TestIssue71ManifestCreationAdmissionAndCancellation`

</details>

<details><summary>internal/server/transfer_recovery_test.go · go · 7 个命名用例</summary>

[源码入口](../../internal/server/transfer_recovery_test.go)

- `TestLargeTransferNonFileReferencesRemainIsolated`
- `TestLargeTransferSparse64BitRange`
- `TestLargeTransferShutdownRejectsLateChunks`
- `TestLargeTransferArchiveStreamsAndRanges`
- `TestIssue64ArchivePreservesDistinctSelectedPaths`
- `TestLargeTransferPauseQueueAndContextBoundary`
- `TestLargeTransferSourceChangesPreserveOriginal`

</details>

<details><summary>internal/server/transfer_sftp_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/transfer_sftp_test.go)

- `TestTransferSFTPProcess`
- `TestLargeTransferSFTPRandomAccessAndResume`

环境引用：`AIDE_TRANSFER_SFTP_HELPER`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/transfer_ssh_live_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/transfer_ssh_live_test.go)

- `TestLargeTransferSSHLive`

环境引用：`AIDE_TRANSFER_SSH_PORT`, `AIDE_TRANSFER_SSH_ROOT`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/tts/clone_test.go · go · 6 个命名用例</summary>

[源码入口](../../internal/server/tts/clone_test.go)

- `TestCloneNotConfigured`
- `TestCloneOpenAICompat`
- `TestCloneUnreachable`
- `TestCloneHTTPError`
- `TestChainCloneFallbackToEdge`
- `TestChainAutoDoesNotIncludeClone`

</details>

<details><summary>internal/server/tts/edge_fragment_test.go · go · 5 个命名用例</summary>

[源码入口](../../internal/server/tts/edge_fragment_test.go)

- `TestIssue97FragmentedMessageHasAggregateLimit`
- `TestIssue97FragmentSequenceAndCountAreBounded`
- `TestIssue97FragmentCountBoundaryAndNextMessage`
- `TestIssue97ValidFragmentsWithInterleavedControlsAndExactLimit`
- `TestIssue97RejectsOversizedContinuationBeforeReadingPayload`

</details>

<details><summary>internal/server/tts/edge_live_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/tts/edge_live_test.go)

- `TestEdgeLiveDial`

环境引用：`RUN_EDGE_LIVE`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/tts/edge_test.go · go · 8 个命名用例</summary>

[源码入口](../../internal/server/tts/edge_test.go)

- `TestDialTimeoutClassification`
- `TestHandshakeTimeoutClassification`
- `TestFirstByteTimeoutClassification`
- `TestRetryOnTimeout`
- `TestNonTimeoutNoRetry`
- `TestVoiceDistinctMD5`
- `TestChainSynthFallback`
- `TestErrEdgeClosedClassified`

环境引用：`RUN_EDGE_LIVE`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/tts/qwen_catalog_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/tts/qwen_catalog_test.go)

- `TestQwenCatalogFiltersProtocolsAndCredentials`
- `TestQwenVoiceArticleSSRAndJSON`
- `TestQwenCatalogFailureKeepsKnownChoices`

</details>

<details><summary>internal/server/tts/qwen_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/tts/qwen_test.go)

- `TestQwenNativeAPI`
- `TestQwenRejectsUnsafeAudio`
- `TestQwenErrorsAndCancel`

</details>

<details><summary>internal/server/tts/sherpa_test.go · go · 5 个命名用例</summary>

[源码入口](../../internal/server/tts/sherpa_test.go)

- `TestSherpaNoModelNotConfigured`
- `TestSherpaDetectFindsModelDir`
- `TestBuildChainPriority`
- `TestChainSynthExplicitSherpaNoModel`
- `TestChainSynthAutoSkipsNoModelSherpa`

环境引用：`SHERPA_TTS_DIR`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>internal/server/update_failure_recovery_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/update_failure_recovery_test.go)

- `TestUpdateAgentFailureResultDoesNotClaimRecovery`

</details>

<details><summary>internal/server/updates_test.go · go · 12 个命名用例</summary>

[源码入口](../../internal/server/updates_test.go)

- `TestCheckUpdatesReturnsVerifiedLauncherLinks`
- `TestStageUpdatePackageAllowsSameVersionAndDowngrade`
- `TestOnlineInstallDownloadsVerifiesAndStagesOfficialBundle`
- `TestCheckUpdatesRequiresAuthentication`
- `TestSourceModeSkipsABUpgrade`
- `TestUpdateAgentSyncClearsStaleSwitchFailureWhenReportingActiveSlot`
- `TestManualSwitchAgentCommandPreservesEmptyOptionalFields`
- `TestUpdateAgentProgressIsVisibleInSlotState`
- `TestReleaseVersionOrdering`
- `TestUploadUpdatePackageAcceptsExtractedFolder`
- `TestUploadUpdatePackageAcceptsZip`
- `TestUploadUpdatePackageAcceptsFullRuntimeZip`

</details>

<details><summary>internal/server/voice_agent_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/voice_agent_test.go)

- `TestVoiceFallbackRecordPlaintext`
- `TestVoiceEncryptLifecycle`
- `TestVoiceAnalyzeRecordsDecision`

</details>

<details><summary>internal/server/voice_clone_bounds_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/voice_clone_bounds_test.go)

- `TestIssue99CloneRejectsDuplicatesAndCountBeforeOutbound`
- `TestIssue99CloneRejectsAggregateAndOversizedStoredSamples`
- `TestIssue99ValidMaximumCountSendsExactSamples`

</details>

<details><summary>internal/server/voice_clone_persistence_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/voice_clone_persistence_test.go)

- `TestIssue153CloneBindingWaitsForPersistentSettings`

</details>

<details><summary>internal/server/voice_fallback_gate_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/voice_fallback_gate_test.go)

- `TestIssue106LockedAssistantCannotUseUnconfiguredVoiceFallback`
- `TestIssue106UnlockedAssistantKeepsNoModelFallback`

</details>

<details><summary>internal/server/voice_history_cipher_test.go · go · 5 个命名用例</summary>

[源码入口](../../internal/server/voice_history_cipher_test.go)

- `TestIssue100SameInstanceUnlockUsesLatestCipher`
- `TestIssue100UnlockedClearSurvivesLockAndRestart`
- `TestIssue100FailedClearPreservesHistoryAndCachedCipher`
- `TestIssue100FailedEncryptionTransitionKeepsPreviousState`
- `TestIssue100LockedHistoryClearIsRejectedWithoutCipherChange`

</details>

<details><summary>internal/server/voice_identity_test.go · go · 10 个命名用例</summary>

[源码入口](../../internal/server/voice_identity_test.go)

- `TestVoiceIdentityContainsName`
- `TestVoiceIdentityFirstPerson`
- `TestVoiceIdentityAideDivision`
- `TestVoiceIdentityCapabilities`
- `TestVoiceIdentityContinuity`
- `TestVoiceIdentityBoundaries`
- `TestVoiceIdentityNameChange`
- `TestIdentityNotAideOrGeneric`
- `TestAnalyzePromptIncludesIdentity`
- `TestNarratePromptIncludesIdentity`

</details>

<details><summary>internal/server/voice_inference_bounds_test.go · go · 5 个命名用例</summary>

[源码入口](../../internal/server/voice_inference_bounds_test.go)

- `TestIssue156InferenceRejectsExcessWorkBeforeModel`
- `TestIssue156CanceledQueuedInferenceDoesNotCallModel`
- `TestIssue156InferenceDeduplicatesSelectedSamples`
- `TestIssue156InferenceAllowsExactAudioAndCountLimits`
- `TestIssue156InferenceBoundsTranscriptBeforeFeatureAndPromptWork`

</details>

<details><summary>internal/server/voice_password_confirmation_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/voice_password_confirmation_test.go)

- `TestIssue149HistoryPasswordRequiresOldSecret`
- `TestIssue149ValidHistoryPasswordRotationPreservesHistory`
- `TestIssue149EmptyCipherStillRequiresActiveSecret`

</details>

<details><summary>internal/server/voice_personality_test.go · go · 6 个命名用例</summary>

[源码入口](../../internal/server/voice_personality_test.go)

- `TestWAVDuration`
- `TestVoicePersonalityAdopt`
- `TestVoicePersonalityAdoptRejectsMissingAnchor`
- `TestVoicePersonalityAdoptEmptyPrompt`
- `TestVoicePersonalityInferNoSamples`
- `TestVoicePersonalityInferFallbackWhenNoLLM`

</details>

<details><summary>internal/server/voice_queue_test.go · go · 13 个命名用例</summary>

[源码入口](../../internal/server/voice_queue_test.go)

- `TestInsertUrgent`
- `TestQueueNewTask`
- `TestQueueNormal`
- `TestQueueFuzzy`
- `TestDefaultModeFallback`
- `TestModeRecorded`
- `TestInsertCooldown`
- `TestAbortBypassesCooldown`
- `TestInterruptedOutputPreserved`
- `TestVoiceFilterSurfacesMode`
- `TestQueueEnqueuedDispatch`
- `TestInsertInterruptsDispatch`
- `TestDefaultQueuePersistent`

</details>

<details><summary>internal/server/voice_sample_boundary_test.go · go · 9 个命名用例</summary>

[源码入口](../../internal/server/voice_sample_boundary_test.go)

- `TestIssue150HTTPHistoryTransitionsReportSaveFailure`
- `TestIssue151LockedAppRejectsVoiceSampleOperations`
- `TestIssue152FailedDeleteRetainsIndex`
- `TestIssue152WipeReportsPartialDeletionAndSupportsRetry`
- `TestIssue152WipeRejectsDirectoryAndIndexIOFailures`
- `TestIssue152UnindexedAudioCannotBeDecrypted`
- `TestIssue151QueuedSampleOperationsRecheckLock`
- `TestIssue152WipeReportsIndexSaveFailure`
- `TestIssue151CorruptLockStateFailsClosed`

</details>

<details><summary>internal/server/voice_sample_index_race_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/voice_sample_index_race_test.go)

- `TestIssue98ConcurrentUploadsKeepEverySuccessfulSample`
- `TestIssue98CanceledMutationsPreserveSamplesAndOtherProfilesProceed`

</details>

<details><summary>internal/server/voice_samples_test.go · go · 6 个命名用例</summary>

[源码入口](../../internal/server/voice_samples_test.go)

- `TestVoiceSampleRequiresUnlock`
- `TestVoiceSampleRequiresConsent`
- `TestVoiceSampleEncryptedStoreAndDelete`
- `TestVoiceCloneCreateBindsVoiceID`
- `TestVoiceCloneCreateNoBaseURL`
- `TestVoiceSamplesWipe`

</details>

<details><summary>internal/server/web_research_test.go · go · 5 个命名用例</summary>

[源码入口](../../internal/server/web_research_test.go)

- `TestWebResearchRoutingAndAttemptGate`
- `TestWebResearchCurrentSchemasOverrideOldHistory`
- `TestWebResearchNoReadAnswerGetsOneCorrection`
- `TestWebResearchLocalDocumentRouting`
- `TestWebResearchLocalComparisonAnswerIsPreserved`

</details>

<details><summary>internal/server/webauthn_test.go · go · 4 个命名用例</summary>

[源码入口](../../internal/server/webauthn_test.go)

- `TestWebAuthnManagerCloseStopsPruner`
- `TestWebAuthnOriginAllowsDynamicLocalPort`
- `TestRegisterStartChallengeKeyMatch`
- `TestAssertionStartChallengeKeyMatch`

</details>

<details><summary>internal/server/workbench_background_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/workbench_background_test.go)

- `TestWorkbenchBackgroundAssetPolicy`

</details>

<details><summary>internal/server/workbench_capability_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/workbench_capability_test.go)

- `TestIssue88StandaloneWorkbenchCapabilitiesGetBridge`

</details>

<details><summary>internal/server/workflow_binding_race_test.go · go · 6 个命名用例</summary>

[源码入口](../../internal/server/workflow_binding_race_test.go)

- `TestIssue157ConcurrentParentReplacementCannotExposeUnboundFiles`
- `TestIssue157ApprovedBoundProposalRejectsRedirectedParent`
- `TestIssue157PinnedDirectoryAndAllReadModes`
- `TestIssue157NewParentsLeafLinksAndApprovalFingerprint`
- `TestIssue157HostBindingGuardKeepsPathAndToolRestrictions`
- `TestIssue157AppliedReadbackKeepsLegacyBinding`

</details>

<details><summary>internal/server/workflow_budget_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/workflow_budget_test.go)

- `TestToolLoopAllowsMoreThan24Calls`
- `TestToolLoopStopsIdenticalRemoteProbesWithExplanation`
- `TestToolCallBudgetFollowsConfiguredRounds`

</details>

<details><summary>internal/server/workflow_clarify_test.go · go · 4 个命名用例</summary>

[源码入口](../../internal/server/workflow_clarify_test.go)

- `TestClarifyRoundIsolation`
- `TestClarifyAnswerToFinishedTask`
- `TestClarifyAnswerNonexistentRun`
- `TestClarifyRoundChannelReplaced`

</details>

<details><summary>internal/server/workflow_parallel_read_test.go · go · 5 个命名用例</summary>

[源码入口](../../internal/server/workflow_parallel_read_test.go)

- `TestParallelReadOnlyToolBatchEligibility`
- `TestSemanticSearchBatchCacheLoadsCorpusOnceConcurrently`
- `TestExecuteIndexedToolBatchRunsConcurrentlyAndKeepsCallOrder`
- `TestToolLoopParallelReadBatchKeepsResultAndAuditOrder`
- `TestToolLoopBatchesLocalSemanticSearches`

</details>

<details><summary>internal/server/workflow_pause_test.go · go · 6 个命名用例</summary>

[源码入口](../../internal/server/workflow_pause_test.go)

- `TestToolLoopCheckpointResumesAfterCompletedTool`
- `TestInterruptedTaskWithCheckpointIsResumableAfterRestart`
- `TestResumeEndpointContinuesSavedMessages`
- `TestPauseEndpointCancelsRunAndPersistsRequest`
- `TestPauseEndpointDoesNotBlockOtherSessions`
- `TestPauseSessionIsolationLiveRuns`

</details>

<details><summary>internal/server/workflow_remote_root_test.go · go · 1 个命名用例</summary>

[源码入口](../../internal/server/workflow_remote_root_test.go)

- `TestIssue563ToolListsRemoteRootOnce`

</details>

<details><summary>internal/server/workflow_shell_approval_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/workflow_shell_approval_test.go)

- `TestRunShellRequiresExactConfirmationForWrites`
- `TestReadOnlyAllowedRejectsShellEvaluation`

</details>

<details><summary>internal/server/workflow_timeout_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/workflow_timeout_test.go)

- `TestTaskExecutionBudgetDefaults`
- `TestTaskResumeRefreshesOnlyModeBudget`

</details>

<details><summary>internal/server/workspace_remote_paths_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/workspace_remote_paths_test.go)

- `TestSFTPNotExistClassification`
- `TestRemoteConfiguredDirectoryPaths`
- `TestSFTPSourceCredentialChangeClosesMaster`

</details>

<details><summary>internal/server/workspace_semantics_test.go · go · 5 个命名用例</summary>

[源码入口](../../internal/server/workspace_semantics_test.go)

- `TestShellPathGuard`
- `TestShellPathGuardDefaultWorkspaceTolerant`
- `TestShellPathGuardSSHNoop`
- `TestCwdPromptLine`
- `TestTaskAgentRootSnapshot`

</details>

<details><summary>internal/server/workspace_startup_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/workspace_startup_test.go)

- `TestWorkspaceStartupMissingLocalPathKeepsConfiguration`
- `TestWorkspaceStartupMalformedConfigStillFails`

</details>

<details><summary>internal/server/workspace_test.go · go · 3 个命名用例</summary>

[源码入口](../../internal/server/workspace_test.go)

- `TestWorkspaceConfigCRUD`
- `TestLocalWorkspacePathAIReadFix`
- `TestRemoteWorkspaceDispatch`

</details>

<details><summary>internal/server/xiaomi_model_selection_test.go · go · 2 个命名用例</summary>

[源码入口](../../internal/server/xiaomi_model_selection_test.go)

- `TestXiaomiConfiguredModelSelection`
- `TestXiaomiModelSourceCompatibility`

</details>

<details><summary>scripts/account-balance-pool.test.cjs · node · 11 个命名用例</summary>

[源码入口](../../scripts/account-balance-pool.test.cjs)

- `only finite supported amounts become balances; unknown is not zero`
- `alarm boundary, negative balances, scale clamping and persisted preference validation`
- `all keys have separate cards and alarms; failures retain stale data without reflecting errors`
- `RAM remains per-key and secrets are cleared before save; successful account stays editable`
- `refresh concurrency is bounded; hidden or detached pool stops requests and aborts`
- `key identity mismatch never populates another card`
- `invalid alert edits are rejected; storage failure keeps session settings with a visible warning`
- `closing the settings sheet clears unsaved RAM fields`
- `plugin quota windows render as percentages without currency amounts or pricing controls`
- `pricing-only and unavailable quota are distinct and unknown never becomes zero`
- `expired quota response cannot populate a current card`

</details>

<details><summary>scripts/assistant-call.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/assistant-call.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/assistant-composer-entry.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/assistant-composer-entry.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/assistant-delegation.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/assistant-delegation.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/assistant-initiative.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/assistant-initiative.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/assistant-live.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/assistant-live.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/assistant-realtime.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/assistant-realtime.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/async-action.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/async-action.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/attachment-source-label.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/attachment-source-label.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/auto-model-router-ui.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/auto-model-router-ui.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/benchmarks/test-stage-timing-analysis.py · python · 3 个命名用例</summary>

[源码入口](../../scripts/benchmarks/test-stage-timing-analysis.py)

- `test_nested_and_parallel_spans_do_not_double_count`
- `test_compaction_outer_span_includes_api_once`
- `test_nanosecond_time_and_worker_union`

</details>

<details><summary>scripts/bootstrap-file-recovery.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/bootstrap-file-recovery.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/browser-assistant-call-review.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-assistant-call-review.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_COMPONENT_EVIDENCE_DIR`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-auto-model-router.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-auto-model-router.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_PLAYWRIGHT`, `AIDE_TEST_URL`, `AUTO_ROUTER_EVIDENCE`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-button-feedback.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-button-feedback.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_EVIDENCE_DIR`, `AIDE_TEST_DATA`, `AIDE_TEST_URL`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-cad-asset-budgets.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-cad-asset-budgets.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_COMPONENT_EVIDENCE_DIR`, `AIDE_EXPECT_REVIEW_FAILURES`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-command-panel.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-command-panel.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_EVIDENCE_DIR`, `AIDE_TEST_DATA`, `AIDE_TEST_URL`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-control-check.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-control-check.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/browser-docx-issue-review.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-docx-issue-review.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_COMPONENT_EVIDENCE_DIR`, `AIDE_DOCX_CATALOG`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-dompurify-stack.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-dompurify-stack.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_COMPONENT_EVIDENCE_DIR`, `AIDE_PURIFY_BASELINE`, `AIDE_PURIFY_STACK_KB`, `AIDE_TEMPLATE_CATALOG`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-drawio-message-origin.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-drawio-message-origin.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_COMPONENT_EVIDENCE_DIR`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-drawio-preview-bounds.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-drawio-preview-bounds.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_COMPONENT_EVIDENCE_DIR`, `AIDE_DRAWIO_AI_ACCEPTANCE`, `AIDE_DRAWIO_ANIMATION_ACCEPTANCE`, `AIDE_DRAWIO_ANIMATION_FOOTER_ACCEPTANCE`, `AIDE_DRAWIO_AWS_SEARCH_ACCEPTANCE`, `AIDE_DRAWIO_BOOTSTRAP_ACCEPTANCE`, `AIDE_DRAWIO_BOOTSTRAP_ASSETS`, `AIDE_DRAWIO_BPMN_PALETTE_ACCEPTANCE`, `AIDE_DRAWIO_CAPTION_METADATA_ACCEPTANCE`, `AIDE_DRAWIO_CLONE_CONSTRAINT_ACCEPTANCE`, `AIDE_DRAWIO_CORS_REVIEW`, `AIDE_DRAWIO_DATA_REVIEW`, `AIDE_DRAWIO_DEVICE_SHAPE_REVIEW`, `AIDE_DRAWIO_EDITING_REVIEW`, `AIDE_DRAWIO_ENTERPRISE_PALETTE_ACCEPTANCE`, `AIDE_DRAWIO_FIFTY_PALETTE_ACCEPTANCE`, `AIDE_DRAWIO_FREEHAND_ACCEPTANCE`, `AIDE_DRAWIO_GEOMETRY_ACCEPTANCE`, `AIDE_DRAWIO_GIF_ACCEPTANCE`, `AIDE_DRAWIO_IMPORT_PLUGIN_REVIEW`, `AIDE_DRAWIO_IMPORT_SECURITY_ACCEPTANCE`, `AIDE_DRAWIO_LAYOUT_ACCEPTANCE`, `AIDE_DRAWIO_LIFECYCLE_ACCEPTANCE`, `AIDE_DRAWIO_LUCID_ACCEPTANCE`, `AIDE_DRAWIO_MERMAID_NATIVE_REVIEW`, `AIDE_DRAWIO_ORGCHART_REVIEW`, `AIDE_DRAWIO_PALETTE_SEARCH_ACCEPTANCE`, `AIDE_DRAWIO_PLANTUML_ISSUE_REVIEW`, `AIDE_DRAWIO_PLANTUML_RESOURCE_REVIEW`, `AIDE_DRAWIO_RESOURCE_REVIEW`, `AIDE_DRAWIO_RUNTIME_ISSUE_ACCEPTANCE`, `AIDE_DRAWIO_SDK_REVIEW`, `AIDE_DRAWIO_SECOND_LAYOUT_ACCEPTANCE`, `AIDE_DRAWIO_SIDEBAR_METADATA_ACCEPTANCE`, `AIDE_DRAWIO_STATE_SEARCH_ACCEPTANCE`, `AIDE_DRAWIO_TEMPLATE_ACCEPTANCE`, `AIDE_DRAWIO_TRACE`, `AIDE_DRAWIO_TRELLO_ACCEPTANCE`, `AIDE_DRAWIO_UI_ROBUSTNESS_REVIEW`, `AIDE_DRAWIO_VISIO_ACCEPTANCE`, `AIDE_DRAWIO_WORKBENCH_PALETTE_ONLY`, `AIDE_DRAWIO_WORKBENCH_REVIEW`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-freehand-corners.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-freehand-corners.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_COMPONENT_EVIDENCE_DIR`, `AIDE_FREEHAND_BASELINE`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-highlight-grammar-review.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-highlight-grammar-review.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_COMPONENT_EVIDENCE_DIR`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-highlight-work-budgets.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-highlight-work-budgets.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_COMPONENT_EVIDENCE_DIR`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-html2canvas-scans.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-html2canvas-scans.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_COMPONENT_EVIDENCE_DIR`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-large-transfers.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-large-transfers.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_EVIDENCE_DIR`, `AIDE_TEST_DATA`, `AIDE_TEST_URL`, `AIDE_TRANSFER_INPUT`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-markdown-boundaries.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-markdown-boundaries.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_COMPONENT_EVIDENCE_DIR`, `AIDE_EXPECT_REVIEW_FAILURES`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-markdown-export-cancel.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-markdown-export-cancel.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_CANVAS_BASELINE`, `AIDE_COMPONENT_EVIDENCE_DIR`, `AIDE_EXPECT_CANCEL_FAILURES`, `AIDE_EXPORT_BASELINE`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-markdown-inline-images.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-markdown-inline-images.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_COMPONENT_EVIDENCE_DIR`, `AIDE_EXPECT_INLINE_FAILURES`, `AIDE_EXPORT_BASELINE`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-markdown-recovery.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-markdown-recovery.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_ASSISTANT_TIMELINE_BASELINE`, `AIDE_COMPONENT_EVIDENCE_DIR`, `AIDE_ISSUE599_SAMPLE`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-marked-link-scans.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-marked-link-scans.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_COMPONENT_EVIDENCE_DIR`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-marked-resource-scans.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-marked-resource-scans.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_COMPONENT_EVIDENCE_DIR`, `AIDE_MARKED_BASELINE`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-mathjax-cases.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-mathjax-cases.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_CASES_BASELINE`, `AIDE_COMPONENT_EVIDENCE_DIR`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-mathjax-issue-review.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-mathjax-issue-review.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_COMPONENT_EVIDENCE_DIR`, `AIDE_EXPECT_REVIEW_FAILURES`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-mathjax-layout-issues.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-mathjax-layout-issues.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_COMPONENT_EVIDENCE_DIR`, `AIDE_EXPECT_REVIEW_FAILURES`, `AIDE_MATH_BASELINE_ROOT`, `AIDE_MATH_OUTPUT`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-mermaid-proof-auth.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-mermaid-proof-auth.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_COMPONENT_EVIDENCE_DIR`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-mermaid-resource-review.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-mermaid-resource-review.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_COMPONENT_EVIDENCE_DIR`, `AIDE_MERMAID_SVG_BASELINE`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-mermaid-svg-issues.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-mermaid-svg-issues.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_COMPONENT_EVIDENCE_DIR`, `AIDE_EXPECT_REVIEW_FAILURES`, `AIDE_MERMAID_SVG_BASELINE`, `AIDE_REVIEW_ISSUES`, `AIDE_REVIEW_RED_ONLY`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-obj-preview-bounds.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-obj-preview-bounds.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_COMPONENT_EVIDENCE_DIR`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-obj-triangle-budget.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-obj-triangle-budget.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_COMPONENT_EVIDENCE_DIR`, `AIDE_OBJ_EXPECT_FAILURES`, `AIDE_OBJ_VENDOR_BASELINE`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-pdf-decoder-followups.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-pdf-decoder-followups.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_COMPONENT_EVIDENCE_DIR`, `AIDE_PDF_BROWSER_CASES`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-pdf-embedded-dimensions.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-pdf-embedded-dimensions.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_COMPONENT_EVIDENCE_DIR`, `AIDE_PDF_BROWSER_CASES`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-pdf-flate-image-budgets.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-pdf-flate-image-budgets.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_COMPONENT_EVIDENCE_DIR`, `AIDE_PDF_BROWSER_CASES`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-pdf-font-annotation-budgets.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-pdf-font-annotation-budgets.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_COMPONENT_EVIDENCE_DIR`, `AIDE_PDF_BROWSER_CASES`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-pdf-font-attachments.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-pdf-font-attachments.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_COMPONENT_EVIDENCE_DIR`, `AIDE_PDF_BROWSER_CASES`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-pdf-font-program-budgets.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-pdf-font-program-budgets.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_COMPONENT_EVIDENCE_DIR`, `AIDE_PDF_BROWSER_CASES`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-pdf-function-depth.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-pdf-function-depth.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_COMPONENT_EVIDENCE_DIR`, `AIDE_PDF_BROWSER_CASES`, `AIDE_PDF_FUNCTION_BASELINE`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-pdf-graphics-fax-budgets.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-pdf-graphics-fax-budgets.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_COMPONENT_EVIDENCE_DIR`, `AIDE_PDF_BROWSER_CASES`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-pdf-jpx-components.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-pdf-jpx-components.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_COMPONENT_EVIDENCE_DIR`, `AIDE_PDF_BROWSER_CASES`, `AIDE_PDF_JPX_BASELINE`, `AIDE_PDF_UPSTREAM_JPX_DIR`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-pdf-page-canvas-budget.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-pdf-page-canvas-budget.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_COMPONENT_EVIDENCE_DIR`, `AIDE_PDF_PAGE_APP_BASELINE`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-pdf-parser-followups.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-pdf-parser-followups.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_COMPONENT_EVIDENCE_DIR`, `AIDE_PDF_BROWSER_CASES`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-pdf-preview-lifecycle.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-pdf-preview-lifecycle.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_COMPONENT_EVIDENCE_DIR`, `AIDE_EXPECT_REVIEW_FAILURES`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-pdf-resource-budgets.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-pdf-resource-budgets.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_COMPONENT_EVIDENCE_DIR`, `AIDE_PDF_BROWSER_CASES`, `AIDE_PDF_BROWSER_WORKER_BASELINE`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-pdf-resource-depth.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-pdf-resource-depth.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_COMPONENT_EVIDENCE_DIR`, `AIDE_PDF_BROWSER_CASES`, `AIDE_PDF_FUNCTION_BASELINE`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-pdf-stream-lifecycle.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-pdf-stream-lifecycle.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_COMPONENT_EVIDENCE_DIR`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-plugin-context-menu.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-plugin-context-menu.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_TEST_DATA`, `AIDE_TEST_URL`, `CHROMIUM_PATH`, `PLUGIN_MENU_EVIDENCE`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-plugin-file-preview.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-plugin-file-preview.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_EVIDENCE_DIR`, `AIDE_TEST_DATA`, `AIDE_TEST_REFERENCE`, `AIDE_TEST_URL`, `AIDE_TEST_WORK`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-plugin-routing.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-plugin-routing.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_EVIDENCE_DIR`, `AIDE_TEST_DATA`, `AIDE_TEST_URL`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-plugin-upgrade-state.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-plugin-upgrade-state.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_EVIDENCE_DIR`, `AIDE_TEST_DATA`, `AIDE_TEST_URL`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-preview-resource-bounds.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-preview-resource-bounds.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_COMPONENT_EVIDENCE_DIR`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-release-matrix.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-release-matrix.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_BROWSER_DEBUG_ENGINE`, `AIDE_EVIDENCE_DIR`, `AIDE_TEST_COMMIT`, `AIDE_TEST_TOKEN`, `AIDE_TEST_URL`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-reminder-schedules.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-reminder-schedules.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_EVIDENCE_DIR`, `AIDE_TEST_DATA`, `AIDE_TEST_URL`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-reminder-voice-regressions.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-reminder-voice-regressions.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_COMPONENT_EVIDENCE_DIR`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-starmap-identity-race.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-starmap-identity-race.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_COMPONENT_EVIDENCE_DIR`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-transfer-snapshot.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-transfer-snapshot.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`CHROME_PATH`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-ui-acceptance-scope.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-ui-acceptance-scope.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/browser-urdf-interaction-budgets.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-urdf-interaction-budgets.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_COMPONENT_EVIDENCE_DIR`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-urdf-lightweight.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-urdf-lightweight.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_EVIDENCE_DIR`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/browser-url-policy.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-url-policy.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/browser-zip-preview-review.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/browser-zip-preview-review.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_COMPONENT_EVIDENCE_DIR`, `AIDE_ZIP_CATALOG`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/ci/test_build_candidate.py · python · 6 个命名用例</summary>

[源码入口](../../scripts/ci/test_build_candidate.py)

- `test_version_and_native_platform`
- `test_archive_reload_is_nonforced_and_rechecks_identity_before_smoke`
- `test_runtime_checks_use_candidate_and_race_uses_separate_toolchain`
- `test_source_excludes_every_installed_plugin`
- `test_all_layers_are_audited_even_deleted_plugin`
- `test_package_identity_checksums_and_data_boundaries`

</details>

<details><summary>scripts/ci/test_ci_policy.py · python · 5 个命名用例</summary>

[源码入口](../../scripts/ci/test_ci_policy.py)

- `test_daily_keeps_gate_without_heavy_builds`
- `test_delivery_is_explicit_and_every_gate_is_required`
- `test_release_browser_has_real_entrypoint_and_receipt_gate`
- `test_host_plan_retains_both_powershells_and_temporary_bridge`
- `test_host_go_requires_actual_assertions_not_only_package_or_skips`

</details>

<details><summary>scripts/ci/test_delivery_gate.py · python · 12 个命名用例</summary>

[源码入口](../../scripts/ci/test_delivery_gate.py)

- `test_branch_candidate_is_read_only_and_cannot_publish`
- `test_both_architectures_required_and_digest_tampering_rejected`
- `test_crossed_or_incomplete_candidate_fails_closed`
- `test_sdk_is_required_and_tampering_rejected`
- `test_malicious_release_ref_rejected_before_remote_operations`
- `test_remote_draft_uses_id_and_rejects_wrong_identity_or_digest`
- `test_weekly_cadence_counts_rcs_ignores_drafts_and_has_exact_boundary`
- `test_one_off_cadence_exception_requires_a_manual_dispatch_and_a_reason`
- `test_cadence_exception_cannot_be_supplied_for_candidate_only`
- `test_notes_require_exact_tag_and_user_facing_sections`
- `test_manual_tag_must_name_current_main_commit`
- `test_host_only_repairs_cannot_publish_a_release`

</details>

<details><summary>scripts/ci/test_inventory.py · python · 0 个命名用例</summary>

[源码入口](../../scripts/ci/test_inventory.py)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/ci/test_public_plugins.py · python · 2 个命名用例</summary>

[源码入口](../../scripts/ci/test_public_plugins.py)

- `test_only_exact_complete_success_is_accepted`
- `test_installation_does_not_substitute_for_function_or_coverage`

</details>

<details><summary>scripts/ci/test_release_evidence.py · python · 3 个命名用例</summary>

[源码入口](../../scripts/ci/test_release_evidence.py)

- `test_every_browser_cell_required_and_bound_to_source`
- `test_all_go_names_need_one_completion_and_skips_are_separate`
- `test_host_must_be_native_and_all_commands_pass`

</details>

<details><summary>scripts/ci/test_snapshot_environment.py · python · 5 个命名用例</summary>

[源码入口](../../scripts/ci/test_snapshot_environment.py)

- `test_current_snapshot_office_overrides_image_environment`
- `test_host_deadline_allows_suite_and_cleanup`
- `test_invalid_parallel_resource_request_never_runs_docker`
- `test_build_stage_full_suite_has_explicit_package_deadline`
- `test_both_helper_families_are_complete_and_tampering_is_rejected`

环境引用：`AIDE_REQUIRE_LAN_BRIDGE`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/ci/test_toolbox_candidate.py · python · 1 个命名用例</summary>

[源码入口](../../scripts/ci/test_toolbox_candidate.py)

- `test_inventory_identity_and_checksum_fail_closed`

</details>

<details><summary>scripts/cmake-render.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/cmake-render.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/codex-host-control.test.cjs · node · 4 个命名用例</summary>

[源码入口](../../scripts/codex-host-control.test.cjs)

- `six supported targets, no unsupported fallback`
- `checksum rejects absent and changed binary`
- `rejects symlink state without modifying destination`
- `arbitrary commands and incomplete launch arguments rejected`

</details>

<details><summary>scripts/context-meter.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/context-meter.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/continuity-isolation.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/continuity-isolation.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/continuity-lock-write.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/continuity-lock-write.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/continuity-retention.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/continuity-retention.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/crash-monitor.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/crash-monitor.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/document-proposals.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/document-proposals.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/drawio-ai-template-browser-cases.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/drawio-ai-template-browser-cases.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/drawio-animation-browser-cases.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/drawio-animation-browser-cases.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/drawio-animation-footer-browser-cases.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/drawio-animation-footer-browser-cases.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/drawio-aws-search-browser-cases.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/drawio-aws-search-browser-cases.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/drawio-bootstrap-palette-browser-cases.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/drawio-bootstrap-palette-browser-cases.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/drawio-bpmn-palette-browser-cases.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/drawio-bpmn-palette-browser-cases.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/drawio-caption-metadata-browser-cases.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/drawio-caption-metadata-browser-cases.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/drawio-clone-constraint-browser-cases.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/drawio-clone-constraint-browser-cases.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/drawio-cors-browser-cases.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/drawio-cors-browser-cases.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/drawio-data-integrity-browser-cases.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/drawio-data-integrity-browser-cases.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_DATA_REVIEW_CATALOG`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/drawio-device-shape-browser-cases.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/drawio-device-shape-browser-cases.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_EXPECT_REVIEW_FAILURES`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/drawio-editing-browser-cases.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/drawio-editing-browser-cases.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_EDIT_REVIEW_CATALOG`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/drawio-enterprise-palette-browser-cases.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/drawio-enterprise-palette-browser-cases.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_ENTERPRISE_CATALOG`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/drawio-fifty-palette-browser-cases.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/drawio-fifty-palette-browser-cases.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/drawio-freehand-browser-cases.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/drawio-freehand-browser-cases.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/drawio-geometry-browser-cases.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/drawio-geometry-browser-cases.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/drawio-gif-browser-cases.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/drawio-gif-browser-cases.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/drawio-import-plugin-browser-cases.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/drawio-import-plugin-browser-cases.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_IMPORT_PLUGIN_REVIEW_CATALOG`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/drawio-import-security-browser-cases.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/drawio-import-security-browser-cases.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_IMPORT_SECURITY_CATALOG`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/drawio-layout-browser-cases.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/drawio-layout-browser-cases.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_DRAWIO_SELECT_TABLE`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/drawio-lifecycle-browser-cases.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/drawio-lifecycle-browser-cases.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/drawio-lucid-message-browser-cases.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/drawio-lucid-message-browser-cases.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/drawio-mermaid-native-browser-cases.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/drawio-mermaid-native-browser-cases.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/drawio-orgchart-browser-cases.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/drawio-orgchart-browser-cases.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_EXPECT_REVIEW_FAILURES`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/drawio-palette-search-browser-cases.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/drawio-palette-search-browser-cases.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/drawio-plantuml-browser-cases.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/drawio-plantuml-browser-cases.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/drawio-plantuml-resource-browser-cases.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/drawio-plantuml-resource-browser-cases.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/drawio-resource-browser-cases.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/drawio-resource-browser-cases.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/drawio-runtime-issue-browser-cases.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/drawio-runtime-issue-browser-cases.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_RUNTIME_CATALOG`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/drawio-sdk-review-browser-cases.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/drawio-sdk-review-browser-cases.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/drawio-second-layout-browser-cases.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/drawio-second-layout-browser-cases.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_SECOND_LAYOUT_BASELINE`, `AIDE_SECOND_LAYOUT_CATALOG`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/drawio-sidebar-metadata-browser-cases.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/drawio-sidebar-metadata-browser-cases.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/drawio-state-search-browser-cases.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/drawio-state-search-browser-cases.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/drawio-template-browser-cases.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/drawio-template-browser-cases.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_TEMPLATE_CATALOG`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/drawio-trello-browser-cases.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/drawio-trello-browser-cases.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/drawio-ui-robustness-browser-cases.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/drawio-ui-robustness-browser-cases.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_EXPECT_REVIEW_FAILURES`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/drawio-visio-archive-browser-cases.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/drawio-visio-archive-browser-cases.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_DRAWIO_MEDIA_ACCEPTANCE`, `AIDE_DRAWIO_NURBS_ACCEPTANCE`, `AIDE_DRAWIO_POLYLINE_ACCEPTANCE`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/drawio-visio-media-browser-cases.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/drawio-visio-media-browser-cases.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/drawio-visio-nurbs-browser-cases.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/drawio-visio-nurbs-browser-cases.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/drawio-visio-polyline-browser-cases.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/drawio-visio-polyline-browser-cases.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/drawio-workbench-browser-cases.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/drawio-workbench-browser-cases.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_WORKBENCH_REVIEW_CATALOG`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/drawio-workbench-palette-browser-cases.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/drawio-workbench-palette-browser-cases.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_WORKBENCH_REVIEW_CATALOG`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/file-autosave-retry.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/file-autosave-retry.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/file-open-request-ownership.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/file-open-request-ownership.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/file-path-navigation.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/file-path-navigation.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/file-recovery-scroll.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/file-recovery-scroll.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/file-route-location.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/file-route-location.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/file-unix-metadata.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/file-unix-metadata.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/generated-media.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/generated-media.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/git-history.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/git-history.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/goldmark-review/main_test.go · go · 3 个命名用例</summary>

[源码入口](../../scripts/goldmark-review/main_test.go)

- `TestEmphasisReferenceSemantics`
- `TestReferenceCompaction`
- `TestParseBoundedInput`

</details>

<details><summary>scripts/identity-login-routing.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/identity-login-routing.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/knowledge-web-parser.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/knowledge-web-parser.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/live-markdown-scan.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/live-markdown-scan.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/lock-cluster-election.test.js · node · 0 个命名用例</summary>

[源码入口](../../scripts/lock-cluster-election.test.js)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/lock-cluster-state.test.js · node · 0 个命名用例</summary>

[源码入口](../../scripts/lock-cluster-state.test.js)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/lock-entry.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/lock-entry.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/lock-recovery.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/lock-recovery.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/management-permissions.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/management-permissions.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/markdown-code.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/markdown-code.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/markdown-diagrams.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/markdown-diagrams.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/markdown-download-export.test.js · node · 0 个命名用例</summary>

[源码入口](../../scripts/markdown-download-export.test.js)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/markdown-history-writable.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/markdown-history-writable.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/markdown-image-path.test.js · node · 0 个命名用例</summary>

[源码入口](../../scripts/markdown-image-path.test.js)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/markdown-inline-image.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/markdown-inline-image.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/markdown-live-preview.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/markdown-live-preview.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/markdown-pdf-layout.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/markdown-pdf-layout.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/markdown-scroll-sync.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/markdown-scroll-sync.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/markdown-template-icons.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/markdown-template-icons.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/mcp-source-form.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/mcp-source-form.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/mermaid-math.test.cjs · node · 10 个命名用例</summary>

[源码入口](../../scripts/mermaid-math.test.cjs)

- `497 embedded renderer is fixed KaTeX release`
- `497 inherited trust cannot enable HTML extensions`
- `499 highest Unicode scalar remains accepted`
- `530 deeply nested groups fail with bounded error`
- `531 finite dimensions are clamped before layout`
- `ordinary math and literal braces still render`
- `530 parser depth cannot be hidden by TeX comments`
- `530 explicit TeX groups share the parser budget`
- `literal braces in verbatim text do not consume group depth`
- `530 depth accounting ignores polluted Object.prototype`

环境引用：`AIDE_MERMAID_SVG_BASELINE`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/message-center.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/message-center.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/message-notifications.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/message-notifications.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/model-token-chart.test.cjs · node · 5 个命名用例</summary>

[源码入口](../../scripts/model-token-chart.test.cjs)

- `99.9 percent retains a separate remainder and proportional ring geometry`
- `single model is a complete ring, not a degenerate arc; invalid data renders empty`
- `small shares preserve every model in details; labels use text, not markup`
- `many long model names keep an HTML legend and keyboard highlight resets`
- `token counts share decimal units and promote rounded boundaries`

</details>

<details><summary>scripts/office/test_docx_comments.py · python · 1 个命名用例</summary>

[源码入口](../../scripts/office/test_docx_comments.py)

- `test_formatted_run_boundary_and_table`

</details>

<details><summary>scripts/outcome-overview-lifecycle.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/outcome-overview-lifecycle.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/outcome-pagination.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/outcome-pagination.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/outcome-request-ownership.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/outcome-request-ownership.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/parallel-chat.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/parallel-chat.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/pause-session-isolation.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/pause-session-isolation.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/plugin-context-menu.test.cjs · node · 6 个命名用例</summary>

[源码入口](../../scripts/plugin-context-menu.test.cjs)

- `native menu is immediate and default/disabled/absent/error/incompatible Compare stays absent`
- `explicit opt-in enables entries in order; both selection actions revalidate before use`
- `revocation after menu render blocks launch and left-side storage, then removes prior selection`
- `late lookup cannot populate closed/replaced menus or cross workspace, token, lock/recovery epoch`
- `lookup failure preserves native actions; context root also uses opt-in`
- `direct stale entry launch rechecks activation, settings and scope`

</details>

<details><summary>scripts/plugin-file-preview.test.cjs · node · 4 个命名用例</summary>

[源码入口](../../scripts/plugin-file-preview.test.cjs)

- `no installed matching handler preserves native path, stale lookups do not launch UI`
- `selected file capability is fixed and disabling active handler restores native`
- `workspace/lock changes close preview without opening an unauthorized native file`
- `transient watch failure retains draft, launch errors and explicit close permit native fallback`

</details>

<details><summary>scripts/plugin-file-review.test.cjs · node · 2 个命名用例</summary>

[源码入口](../../scripts/plugin-file-review.test.cjs)

- `standalone project review leaves file route and exposes host approval without discarding other history`
- `ordinary workbench review keeps route and view state intact`

</details>

<details><summary>scripts/plugin-market.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/plugin-market.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/plugin-plans.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/plugin-plans.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/plugin-upgrade.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/plugin-upgrade.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/plugin-workbench-client.test.cjs · node · 5 个命名用例</summary>

[源码入口](../../scripts/plugin-workbench-client.test.cjs)

- `client accepts only parent handshake for its plugin, calls declared tools, and preserves pending status`
- `scope revocation rejects outstanding call without issuing a retry`
- `document APIs encode bounded binary chunks without roots, tokens, or an iframe commit API`
- `throwing event listeners cannot prevent scope revocation`
- `capture SDK uses the existing authenticated MessagePort without credentials or a new HTTP client`

</details>

<details><summary>scripts/plugin-workbench.test.cjs · node · 20 个命名用例</summary>

[源码入口](../../scripts/plugin-workbench.test.cjs)

- `workbench uses sandbox without same-origin or credentials and close unloads the frame`
- `lock, scope changes, disabled plugins and Escape close the active workbench`
- `workbench refuses arbitrary routes returned by a broken API`
- `MessagePort only connects to the active frame and never carries parent instance/auth`
- `closing a workbench drops late tool replies and proposal approval is absent from the port`
- `workbench export requires actual active frame and a separate parent save click; cleanup revokes data`
- `workbench export rejects traversal, foreign plugin, oversized data and replaces pending export; revocation prevents save`
- `theme follows host only after a bound frame requests it and stops on close`
- `idle renewal supports more than 128 calls without exposing rotated keys or retrying tools`
- `daemon detach closes the view immediately and drains one dispatched call; revocation still aborts`
- `page exit revokes its own bridge and unloads the sandbox`
- `workbench stays embedded without new-tab controller`
- `file selection hints contain only validated source paths and no credentials`
- `file bridge rejects iframe commit; only trusted parent Save publishes and emits saved`
- `leaving a dirty file viewer can be cancelled before removing the frame`
- `mobile collection is opt-in and cannot invoke undeclared methods or disclose parent credentials`
- `employee and location capabilities require an explicit compatible capture protocol`
- `review preserves iframe draft and bridge, returns on approved save, and still revokes on scope loss`
- `manual project save writes directly and never permits iframe commit`
- `Codex host capability requires both the exact plugin and parent grant`

</details>

<details><summary>scripts/plugin-workspace.test.cjs · node · 2 个命名用例</summary>

[源码入口](../../scripts/plugin-workspace.test.cjs)

- `plugin helper uses bound project, rejects traversal and symlink escape, and denies empty remote root`
- `daemon helper binds each overlapping call root independently without global chdir`

</details>

<details><summary>scripts/project-file-workbench.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/project-file-workbench.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/qwen-tts.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/qwen-tts.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/reasoning-settings.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/reasoning-settings.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/recovery-transfer.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/recovery-transfer.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/run-activity-cards.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/run-activity-cards.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/run-stop-ownership.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/run-stop-ownership.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/runtime-lifecycle.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/runtime-lifecycle.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/session-model-selection.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/session-model-selection.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/session-render-cache.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/session-render-cache.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/session-run-controls.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/session-run-controls.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/settings-navigation.test.cjs · node · 5 个命名用例</summary>

[源码入口](../../scripts/settings-navigation.test.cjs)

- `all original settings remain reachable exactly once in seven groups`
- `new sections and unknown legacy links have a safe reachable destination`
- `advanced controls initialize once only after expansion and keep original renderer`
- `appearance uses independent palette and mode with unchanged stored values`
- `opening voice settings never requests microphone permission; explicit refresh stops its temporary track`

</details>

<details><summary>scripts/source-relay-ui.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/source-relay-ui.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/stable-markdown-cache.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/stable-markdown-cache.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/staged-output.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/staged-output.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/starmap-ai-exploration.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/starmap-ai-exploration.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/starmap-background.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/starmap-background.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/starmap-cache.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/starmap-cache.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/starmap-coverage-state.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/starmap-coverage-state.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/starmap-document-continuation.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/starmap-document-continuation.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/starmap-empty-state.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/starmap-empty-state.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/starmap-file-preview.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/starmap-file-preview.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/starmap-frame-work.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/starmap-frame-work.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/starmap-gates.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/starmap-gates.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/starmap-gravity.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/starmap-gravity.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/starmap-infrared.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/starmap-infrared.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/starmap-lensing.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/starmap-lensing.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/starmap-live-metadata.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/starmap-live-metadata.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/starmap-local-frame.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/starmap-local-frame.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/starmap-logbook.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/starmap-logbook.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/starmap-outline.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/starmap-outline.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/starmap-path.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/starmap-path.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/starmap-progressive-cadence.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/starmap-progressive-cadence.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/starmap-stellar-scale.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/starmap-stellar-scale.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/starmap-timeline.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/starmap-timeline.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/starmap-topics.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/starmap-topics.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/starmap-workspace-scene.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/starmap-workspace-scene.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/steer-user-bubbles.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/steer-user-bubbles.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/task-action-hub.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/task-action-hub.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test-large-transfer-ssh.py · python · 0 个命名用例</summary>

[源码入口](../../scripts/test-large-transfer-ssh.py)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_accessibility_port.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_accessibility_port.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_agent_route.py · python · 6 个命名用例</summary>

[源码入口](../../scripts/test_agent_route.py)

- `test_snapshot_owner_outlives_its_execution_and_cleanup_deadlines`
- `test_public_runtime_asset_gate_fails_closed`
- `test_explicit_interpreter_overrides`
- `test_task_path_rejects_escape`
- `test_release_gate`
- `test_cleanup_preserves_unknown_protected_tracked_and_symlinks`

</details>

<details><summary>scripts/test_agent_skill_routing.py · python · 7 个命名用例</summary>

[源码入口](../../scripts/test_agent_skill_routing.py)

- `test_router_limits_matches_and_all_skill_files_exist`
- `test_backend_request_selects_backend_as_primary`
- `test_frontend_request_selects_frontend_as_primary`
- `test_each_specialist_route_has_a_representative_request`
- `test_unmatched_request_falls_back_to_coordinator`
- `test_command_prints_primary_skill_and_does_not_spawn_agents`
- `test_prompt_request_keeps_client_entry_and_adds_routing`

</details>

<details><summary>scripts/test_approval_persistence.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_approval_persistence.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_assistant_mode_controls.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_assistant_mode_controls.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_assistant_result_refresh.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_assistant_result_refresh.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_avatar_feedback.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_avatar_feedback.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_avatar_pack_paths.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_avatar_pack_paths.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_avatar_player.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_avatar_player.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_avatar_settings.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_avatar_settings.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_browser_pdf.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_browser_pdf.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_cad_light_aggregate.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_cad_light_aggregate.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_URDF_REVIEW_BASELINE`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/test_code_layout.py · python · 5 个命名用例</summary>

[源码入口](../../scripts/test_code_layout.py)

- `test_navigation_documents_and_local_links_exist`
- `test_package_and_embedded_ui_boundaries_match_the_map`
- `test_stable_host_entrypoints_remain_at_their_documented_paths`
- `test_mock_services_are_in_fixtures_and_docker_copy_resolves`
- `test_active_docs_and_source_comments_use_new_mock_paths`

</details>

<details><summary>scripts/test_context_compaction_ui.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_context_compaction_ui.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_control_bridge_launchers.py · python · 1 个命名用例</summary>

[源码入口](../../scripts/test_control_bridge_launchers.py)

- `test_start_repeat_and_stop`

</details>

<details><summary>scripts/test_control_plugin_runtime.py · python · 0 个命名用例</summary>

[源码入口](../../scripts/test_control_plugin_runtime.py)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_development_toolbox.py · python · 3 个命名用例</summary>

[源码入口](../../scripts/test_development_toolbox.py)

- `test_explicit_argv_workspace_only_and_network_opt_in`
- `test_export_preserves_existing_files`
- `test_default_release_has_fresh_base_and_toolbox_is_separate`

</details>

<details><summary>scripts/test_docker_release.py · python · 5 个命名用例</summary>

[源码入口](../../scripts/test_docker_release.py)

- `test_success_preserves_existing_container_and_cleans_owned_id`
- `test_health_failure_preserves_foreign_and_never_exports`
- `test_name_collision_has_no_owned_container_to_remove`
- `test_failed_run_with_cid_still_cleans_only_owned_container`
- `test_invalid_cid_never_becomes_a_cleanup_target`

</details>

<details><summary>scripts/test_docx_preview_bounds.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_docx_preview_bounds.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_drawio_ai_templates.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_drawio_ai_templates.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_drawio_animation_validation.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_drawio_animation_validation.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_drawio_clone_constraints.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_drawio_clone_constraints.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_DRAWIO_BUNDLE_ONLY`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/test_drawio_cors_linear.cjs · node · 2 个命名用例</summary>

[源码入口](../../scripts/test_drawio_cors_linear.cjs)

- `x`
- `https://`

环境引用：`AIDE_CORS_BASELINE`, `AIDE_CORS_WITNESS_ONLY`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/test_drawio_freehand_preview.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_drawio_freehand_preview.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_drawio_geometry_fixes.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_drawio_geometry_fixes.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_DRAWIO_COPY`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/test_drawio_gif_frames.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_drawio_gif_frames.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_drawio_layout_fixes.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_drawio_layout_fixes.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_DRAWIO_BUNDLE_ONLY`, `AIDE_DRAWIO_SKIP_LAYOUT_FIXES`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/test_drawio_lifecycle_alpha.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_drawio_lifecycle_alpha.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_DRAWIO_COPY`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/test_drawio_palette_search.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_drawio_palette_search.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_drawio_png_import.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_drawio_png_import.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_PNG_CATALOG`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/test_drawio_preview_bounds.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_drawio_preview_bounds.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_drawio_trello_limits.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_drawio_trello_limits.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_drawio_ui_fallback.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_drawio_ui_fallback.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_DRAWIO_EDITING_BASELINE`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/test_drawio_visio_archive.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_drawio_visio_archive.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_drawio_visio_media_errors.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_drawio_visio_media_errors.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_drawio_visio_nurbs.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_drawio_visio_nurbs.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_drawio_visio_polyline.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_drawio_visio_polyline.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_dxf_preview_bounds.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_dxf_preview_bounds.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_file_type_detection.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_file_type_detection.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_frame_interpolation.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_frame_interpolation.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_INTERPOLATION_FIXTURE_AUDIT_FAIL`, `AIDE_INTERPOLATION_FIXTURE_FAIL`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/test_go_snapshot.py · python · 0 个命名用例</summary>

[源码入口](../../scripts/test_go_snapshot.py)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_TEST_EVIDENCE_DIR`, `AIDE_TEST_IMAGE`, `AIDE_TEST_TOOLCHAIN_IMAGE`, `AIDE_TEST_WORKERS`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/test_go_test_shards.py · python · 4 个命名用例</summary>

[源码入口](../../scripts/test_go_test_shards.py)

- `test_receipts_keep_skips_separate_and_missing_execution_fails`
- `test_inventory_preserves_examples_and_fuzz_seeds`
- `test_default_workers_do_not_overlap`
- `test_all_shards_finish_and_failure_is_not_hidden`

</details>

<details><summary>scripts/test_headless_browser.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_headless_browser.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_headless_websocket.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_headless_websocket.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`CHROME_PATH`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/test_highlight_grammar_review.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_highlight_grammar_review.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_HIGHLIGHT_CATALOG`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/test_html2canvas_scans.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_html2canvas_scans.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_i18n.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_i18n.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_i18n_coverage.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_i18n_coverage.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_install.py · python · 4 个命名用例</summary>

[源码入口](../../scripts/test_install.py)

- `test_check_does_not_mutate_configuration`
- `test_source_keeps_existing_configuration`
- `test_image_initializes_versioned_configuration`
- `test_bad_checksum_never_loads_or_starts`

</details>

<details><summary>scripts/test_integrity_upgrade_docker.py · python · 0 个命名用例</summary>

[源码入口](../../scripts/test_integrity_upgrade_docker.py)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_launcher_clean_environment.py · python · 0 个命名用例</summary>

[源码入口](../../scripts/test_launcher_clean_environment.py)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_launcher_contract.py · python · 23 个命名用例</summary>

[源码入口](../../scripts/test_launcher_contract.py)

- `test_slow_fingerprint_shows_progress_without_polluting_digest`
- `test_fingerprint_timeout_kills_pipeline_descendants`
- `test_build_is_explicit_scoped_and_starts_verified_source`
- `test_build_executes_real_disk_child_without_inherited_project_dir`
- `test_failed_build_cannot_switch_image_or_start_release`
- `test_wrong_build_identity_retains_previous_configuration`
- `test_other_builder_is_never_adopted`
- `test_separate_checkouts_get_separate_image_and_cache_identity`
- `test_release_package_rejects_source_commands_before_docker`
- `test_cleanup_preserves_data_and_never_prunes_global_resources`
- `test_foreign_project_refuses_cleanup_before_mutation`
- `test_shared_image_is_retained`
- `test_dry_cleanup_only_reads_and_does_not_touch_state`
- `test_symlink_build_state_is_rejected_before_mutation`
- `test_verify_missing_docker_reports_failure_without_start_or_build`
- `test_verify_stopped_runtime_is_diagnostic_only`
- `test_release_refuses_dirty_main_or_wrong_tag_without_dispatch`
- `test_release_uses_existing_notes_and_current_gate`
- `test_healthy_source_start_ignores_legacy_compile_switches`
- `test_missing_runtime_refuses_instead_of_compiling`
- `test_stale_bundle_marker_cannot_select_release_or_trigger_import`
- `test_build_completion_cannot_reopen_previous_healthy_image`
- `test_actual_packager_ships_only_release_interfaces`

</details>

<details><summary>scripts/test_launcher_data.py · python · 40 个命名用例</summary>

[源码入口](../../scripts/test_launcher_data.py)

- `test_dotenv_compose_files_survive_generated_mount_override`
- `test_exported_compose_files_keep_precedence_without_resolving_dotenv`
- `test_default_and_resolution_failure`
- `test_import_preserves_bytes_modes_links_and_survives_source_removal`
- `test_fresh_auth_mount_is_prepared_without_user_data_access`
- `test_transient_fifo_socket_are_not_copied`
- `test_windows_import_sets_container_user_ownership`
- `test_windows_matching_auth_ownership_does_not_chown`
- `test_windows_existing_storage_repairs_root_ownership`
- `test_unverified_existing_destination_is_never_overwritten`
- `test_symlink_root_storage_and_receipt_are_rejected`
- `test_bad_copy_never_commits_and_retains_source_and_stage`
- `test_simultaneous_launchers_commit_once`
- `test_missing_image_marker_restores_exact_identity`
- `test_missing_environment_restores_literal_secret_configuration`
- `test_linked_environment_is_rejected`
- `test_persistent_data_excluded_from_build_and_git`
- `test_select_only_intended_service_owner_and_refuse_ambiguity`
- `test_existing_image_needs_no_download_or_load`
- `test_local_archive_imports_without_network_or_build`
- `test_missing_archive_downloads_verifies_and_streams`
- `test_bad_checksum_never_loads_or_retags`
- `test_wrong_platform_and_untrusted_url_never_download`
- `test_duplicate_manifest_is_refused`
- `test_source_seed_uses_local_bundle_without_activating_old_runtime`
- `test_missing_source_seed_archive_never_downloads_or_changes_tags`
- `test_invalid_source_seed_fails_without_fallback`
- `test_source_seed_rejects_dangling_archive_or_partial_symlink`
- `test_source_seed_import_error_is_not_classified_as_missing_archive`
- `test_installed_source_seed_needs_no_archive`
- `test_duplicate_source_seed_manifest_never_imports`
- `test_successful_build_does_not_contact_release`
- `test_failed_source_build_never_substitutes_release`
- `test_legacy_fallback_flag_cannot_hide_source_failure`
- `test_unused_release_receipt_cannot_change_build_outcome`
- `test_source_and_windows_standard_entries_use_explicit_build_policy`
- `test_source_checkout_is_not_hidden_by_stale_bundle_marker`
- `test_source_selection_respects_checkout_and_explicit_override`
- `test_running_instance_requires_local_service_health`
- `test_installed_runtime_requires_platform_and_complete_metadata`

环境引用：`FIX_LOAD_EXIT`, `FIX_RELEASE_RECEIPT`, `FIX_STATE`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/test_launcher_data_docker.py · python · 0 个命名用例</summary>

[源码入口](../../scripts/test_launcher_data_docker.py)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_libavoid_readiness.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_libavoid_readiness.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_EXPECT_REVIEW_FAILURES`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/test_local_dependency_image.py · python · 9 个命名用例</summary>

[源码入口](../../scripts/test_local_dependency_image.py)

- `test_existing_base_is_reused_without_build_or_load`
- `test_wrong_architecture_is_rejected`
- `test_missing_base_is_loaded_from_verified_local_archive`
- `test_corrupt_archive_never_loads_or_builds`
- `test_local_seed_build_has_no_network_or_pull`
- `test_no_seed_reports_bootstrap_only`
- `test_old_cad_seed_is_rejected_without_build`
- `test_slim_seed_reports_development_environment_required`
- `test_failed_local_build_is_not_replaced_with_online_build`

环境引用：`BASE_PLATFORM`, `BUILD_EXIT`, `NO_SEED`, `OLD_CAD`, `SLIM_SEED`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/test_local_root.py · python · 3 个命名用例</summary>

[源码入口](../../scripts/test_local_root.py)

- `test_preserves_settings_and_quotes_spaces`
- `test_macos_root_and_return_to_scoped_directory`
- `test_invalid_root_keeps_configuration`

</details>

<details><summary>scripts/test_maintenance.py · python · 10 个命名用例</summary>

[源码入口](../../scripts/test_maintenance.py)

- `test_stop_exact_root_and_service`
- `test_existing_cli_dispatch_uses_same_scoped_stop`
- `test_already_stopped_is_idempotent`
- `test_dry_run_does_not_stop_or_write`
- `test_stop_marker_blocks_port_watcher`
- `test_cache_only_builder_no_docker_data`
- `test_cache_preview`
- `test_engine_unavailable_still_stops_owned_helpers_and_blocks_restart`
- `test_helper_failure_still_stops_application_containers`
- `test_errors_propagate`

环境引用：`ENGINE_EXIT`, `IDS`, `PRUNE_EXIT`, `STOP_EXIT`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/test_marked_link_scans.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_marked_link_scans.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_EXPECT_REVIEW_FAILURES`, `AIDE_MARKED_CONTROLS`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/test_marked_resource_scans.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_marked_resource_scans.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_MARKED_BASELINE`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/test_mermaid_native_issues.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_mermaid_native_issues.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_EXPECT_REVIEW_FAILURES`, `AIDE_NATIVE_REVIEW_BASELINE`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/test_mermaid_proof_auth.py · python · 5 个命名用例</summary>

[源码入口](../../scripts/test_mermaid_proof_auth.py)

- `test_cross_origin_simple_post_cannot_overwrite_proof`
- `test_valid_token_and_same_origin_writes_receipt`
- `test_token_cannot_bypass_origin_or_media_type`
- `test_malformed_or_oversized_proof_does_not_replace_receipt`
- `test_fixture_response_retains_csp_and_restricts_cross_origin_scripts`

</details>

<details><summary>scripts/test_message_lan_launcher.py · python · 5 个命名用例</summary>

[源码入口](../../scripts/test_message_lan_launcher.py)

- `test_start_uses_loopback_mapping_and_pristine_checked_image`
- `test_mismatched_checksum_does_not_execute_helper`
- `test_non_loopback_mapping_rejected`
- `test_capture_target_cannot_expand_to_non_loopback`
- `test_both_images_and_all_launch_modes_ship_bridge`

环境引用：`LAN_BAD_HASH`, `LAN_BINDING`, `LAN_CAPTURE_BINDING`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/test_mixsoul_embedding_follow.py · python · 6 个命名用例</summary>

[源码入口](../../scripts/test_mixsoul_embedding_follow.py)

- `test_restart_rebinds_and_retains_credentials_off_command_line`
- `test_stopped_or_absent_main_has_no_mutation`
- `test_foreign_project_or_sidecar_is_retained`
- `test_remote_endpoint_or_disabled_engine_is_refused`
- `test_main_replacement_during_inspection_has_no_mutation`
- `test_source_and_release_compose_forward_opt_in_without_shell_evaluation`

</details>

<details><summary>scripts/test_native_distribution.py · python · 3 个命名用例</summary>

[源码入口](../../scripts/test_native_distribution.py)

- `test_matching_architectures`
- `test_wrong_architectures`
- `test_malformed_headers`

</details>

<details><summary>scripts/test_obj_preview_bounds.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_obj_preview_bounds.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_office_extraction_bounds.py · python · 4 个命名用例</summary>

[源码入口](../../scripts/test_office_extraction_bounds.py)

- `test_docx_stops_producing_paragraphs`
- `test_xlsx_closes_stream_after_truncation`
- `test_pdf_stops_producing_pages`
- `test_pptx_positive_control_and_bounds`

</details>

<details><summary>scripts/test_offline_bundle.py · python · 15 个命名用例</summary>

[源码入口](../../scripts/test_offline_bundle.py)

- `test_first_start_loads_checked_image_without_building`
- `test_repeat_start_keeps_configuration_and_skips_load`
- `test_architecture_mismatch_stops_before_loading`
- `test_invalid_published_ports_never_report_ready_or_open`
- `test_failed_port_query_never_reports_ready`
- `test_host_connection_or_certificate_failure_never_opens`
- `test_other_host_instance_never_opens`
- `test_missing_container_identity_never_opens_or_probes`
- `test_missing_saved_certificate_never_opens_or_probes`
- `test_ipv4_and_ipv6_bindings_share_one_verified_port`
- `test_corrupt_archive_never_loads`
- `test_wrong_loaded_identity_never_starts`
- `test_cross_engine_id_resolves_only_current_import_reference`
- `test_cross_engine_ambiguous_or_wrong_arch_import_stops`
- `test_release_image_download_resumes_after_interruption`

环境引用：`CONTAINER_INSTANCE`, `ENGINE_PLATFORM`, `HOST_HEALTH_EXIT`, `HOST_INSTANCE`, `LOADED_ID`, `LOADED_PLATFORM`, `LOAD_OUTPUT`, `NO_HOST_CERT`, `PORT_EXIT`, `PORT_OUTPUT`, `PUBLISHER_ID_MISSING`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/test_optional_step.py · python · 0 个命名用例</summary>

[源码入口](../../scripts/test_optional_step.py)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_TEST_IMAGE`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/test_orgchart_assembly_mirror.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_orgchart_assembly_mirror.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_parser_layout_resources.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_parser_layout_resources.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_EXPECT_REVIEW_FAILURES`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/test_password_prompt.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_password_prompt.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_pip_install_retry.py · python · 3 个命名用例</summary>

[源码入口](../../scripts/test_pip_install_retry.py)

- `test_first_success_is_not_repeated`
- `test_partial_download_failure_retries_whole_install_without_changing_index_or_pins`
- `test_permanent_failure_is_bounded_and_preserves_exit_status`

</details>

<details><summary>scripts/test_plantuml_converter_issues.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_plantuml_converter_issues.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_EXPECT_REVIEW_FAILURES`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/test_plugin_isolation.py · python · 1 个命名用例</summary>

[源码入口](../../scripts/test_plugin_isolation.py)

- `test_migrate_then_upgrade_and_pull`

</details>

<details><summary>scripts/test_prebuild_wheels.py · python · 6 个命名用例</summary>

[源码入口](../../scripts/test_prebuild_wheels.py)

- `test_failed_download_preserves_last_known_good`
- `test_failed_resolver_verification_preserves_last_known_good`
- `test_success_replaces_only_after_download_and_verification`
- `test_empty_success_rejected`
- `test_failed_promotion_restores_previous_directory`
- `test_existing_lock_refuses_other_run_without_touching_inputs`

</details>

<details><summary>scripts/test_prebuilt_publication.py · python · 7 个命名用例</summary>

[源码入口](../../scripts/test_prebuilt_publication.py)

- `test_export_records_candidate_without_changing_public_defaults`
- `test_matching_public_pair_passes_without_writing`
- `test_missing_duplicate_draft_or_wrong_release_fail`
- `test_public_metadata_must_match_digest_url_size_and_upload_state`
- `test_incomplete_or_mixed_source_pair_fails_before_network`
- `test_untrusted_url_or_bad_identity_is_rejected_without_echoing_secrets`
- `test_network_failure_propagates_without_changing_manifest`

</details>

<details><summary>scripts/test_prompt_shortcut.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_prompt_shortcut.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_reference_relay_agent.py · python · 9 个命名用例</summary>

[源码入口](../../scripts/test_reference_relay_agent.py)

- `test_unicode_read_write_and_exclusive`
- `test_interrupted_and_corrupt_upload_preserve_original`
- `test_root_traversal_and_protocol_paths`
- `test_script_hardlink_and_private_directory_hidden`
- `test_symlink_file_and_parent_rejected`
- `test_special_files_do_not_block`
- `test_listing_and_header_bounds_fail_explicitly`
- `test_atomic_no_replace_rename`
- `test_handle_limits_close_and_checksum_before_write`

</details>

<details><summary>scripts/test_reminder_popup.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_reminder_popup.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_reminder_styles.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_reminder_styles.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_resource_guard.py · python · 9 个命名用例</summary>

[源码入口](../../scripts/test_resource_guard.py)

- `test_disk_guard_without_exported_project_dir_uses_current_directory`
- `test_explicit_disk_path_overrides_stale_parent_environment`
- `test_low_disk_stops_before_mutation`
- `test_build_reserves_more_space_than_import`
- `test_low_engine_memory_stops_before_mutation`
- `test_unknown_disk_and_engine_reports_fail_closed`
- `test_real_launcher_rejects_low_disk_before_migration_or_docker`
- `test_snapshot_launch_is_bounded_and_keeps_sharded_command`
- `test_healthy_budget_allows_start`

</details>

<details><summary>scripts/test_runtime_assets.py · python · 0 个命名用例</summary>

[源码入口](../../scripts/test_runtime_assets.py)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_REQUIRE_LAN_BRIDGE`, `AIDE_RUNTIME_PROFILE`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/test_source_fingerprint.py · python · 3 个命名用例</summary>

[源码入口](../../scripts/test_source_fingerprint.py)

- `test_native_manifest_under_ascii_locale`
- `test_caller_collation_does_not_change_source_identity`
- `test_installed_plugin_state_is_excluded`

</details>

<details><summary>scripts/test_starmap_identity_race.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_starmap_identity_race.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_stream_url.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_stream_url.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_tts_archive_integrity.py · python · 9 个命名用例</summary>

[源码入口](../../scripts/test_tts_archive_integrity.py)

- `test_verify_explicit_trusted_digest`
- `test_missing_or_invalid_digest_fails`
- `test_known_name_cannot_bypass_pin`
- `test_valid_install_has_required_files`
- `test_modified_model_does_not_install`
- `test_modified_espeak_does_not_install`
- `test_missing_phonemes_does_not_install`
- `test_missing_tokens_does_not_install`
- `test_missing_onnx_does_not_install`

</details>

<details><summary>scripts/test_ui_acceptance_scope.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_ui_acceptance_scope.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_ui_calm.py · python · 5 个命名用例</summary>

[源码入口](../../scripts/test_ui_calm.py)

- `test_wide_settings_sheet_gets_later_lower_blur_override`
- `test_reduced_transparency_covers_wide_settings_sheet`
- `test_focus_keyboard_reduced_motion_and_desktop_layout_remain_present`
- `test_theme_custom_property_contracts_are_unchanged`
- `test_html_cache_version_advances_with_macos_stylesheet`

</details>

<details><summary>scripts/test_ui_click_latency.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_ui_click_latency.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_upload_progress.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_upload_progress.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_voice_panel.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_voice_panel.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_voice_sample_duration.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_voice_sample_duration.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_windows_bat_entrypoints.py · python · 0 个命名用例</summary>

[源码入口](../../scripts/test_windows_bat_entrypoints.py)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_windows_build_proxy.py · python · 0 个命名用例</summary>

[源码入口](../../scripts/test_windows_build_proxy.py)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_windows_directory_launcher.py · python · 0 个命名用例</summary>

[源码入口](../../scripts/test_windows_directory_launcher.py)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_windows_image_slot.py · python · 0 个命名用例</summary>

[源码入口](../../scripts/test_windows_image_slot.py)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_windows_launcher_acl.py · python · 0 个命名用例</summary>

[源码入口](../../scripts/test_windows_launcher_acl.py)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_windows_launcher_compile.py · python · 0 个命名用例</summary>

[源码入口](../../scripts/test_windows_launcher_compile.py)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_windows_native_host.py · python · 0 个命名用例</summary>

[源码入口](../../scripts/test_windows_native_host.py)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_windows_native_launcher.py · python · 0 个命名用例</summary>

[源码入口](../../scripts/test_windows_native_launcher.py)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_windows_native_package.py · python · 11 个命名用例</summary>

[源码入口](../../scripts/test_windows_native_package.py)

- `test_only_fixed_member_is_extracted`
- `test_full_release_uses_verified_nested_image`
- `test_full_release_rejects_wrong_image_digest`
- `test_missing_or_duplicate_member`
- `test_symlink_and_special_entries_rejected`
- `test_existing_destination_retained`
- `test_crc_failure_and_truncation_never_succeed`
- `test_encrypted_and_oversize_entries_rejected`
- `test_invalid_name_rejected_before_io`
- `test_idle_and_terminal_runs`
- `test_active_unknown_malformed_and_flat_legacy_sessions`

</details>

<details><summary>scripts/test_windows_native_portable.py · python · 0 个命名用例</summary>

[源码入口](../../scripts/test_windows_native_portable.py)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_windows_port_lifecycle.py · python · 5 个命名用例</summary>

[源码入口](../../scripts/test_windows_port_lifecycle.py)

- `test_launcher_selects_and_persists_fallback_port`
- `test_launcher_starts_host_port_watcher_for_offline_bundles`
- `test_watcher_applies_saved_port_without_changing_shared_data_mounts`
- `test_host_agents_keep_launcher_compose_overrides`
- `test_native_entrypoints_and_packaging_replace_legacy_agents`

</details>

<details><summary>scripts/test_windows_prebuilt_import.py · python · 0 个命名用例</summary>

[源码入口](../../scripts/test_windows_prebuilt_import.py)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_windows_startup_recovery.py · python · 0 个命名用例</summary>

[源码入口](../../scripts/test_windows_startup_recovery.py)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`FIXTURE_CACHE`, `FIXTURE_FRESH_OK`, `FIXTURE_NATIVE_LOG`, `FIXTURE_STOP`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/test_windows_update_agent.py · python · 0 个命名用例</summary>

[源码入口](../../scripts/test_windows_update_agent.py)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_windows_update_launcher_process.py · python · 0 个命名用例</summary>

[源码入口](../../scripts/test_windows_update_launcher_process.py)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/test_xsys_platform_contracts.py · python · 11 个命名用例</summary>

[源码入口](../../scripts/test_xsys_platform_contracts.py)

- `test_292_cpu_flags_are_independent`
- `test_293_netbsd_missing_cpu_node`
- `test_293_300_netbsd_empty_node_queries`
- `test_296_tee_ignores_second_register`
- `test_297_freebsd_reserved_capability`
- `test_298_generator_checkout_with_spaces`
- `test_301_bsd_xattr_error_propagation`
- `test_302_windows_descriptor_error`
- `test_303_windows_working_set_bits`
- `test_304_temporary_sockets_close_once`
- `test_305_success_bool_partial_privilege`

环境引用：`AIDE_TEST_GO`, `AIDE_XSYS_ROOT`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/test_zip_preview_bounds.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/test_zip_preview_bounds.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/toast-lifecycle.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/toast-lifecycle.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/tts-playback.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/tts-playback.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/ui-response-latency.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/ui-response-latency.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/ui_acceptance_scope.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/ui_acceptance_scope.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/ui_queue_check.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/ui_queue_check.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_TOKEN`, `BASE_URL`, `CHROME_PATH`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/ui_stream_check.cjs · browser · 0 个命名用例</summary>

[源码入口](../../scripts/ui_stream_check.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

环境引用：`AIDE_TOKEN`, `BASE_URL`, `CHROME_PATH`。缺少配置时可能跳过；必须核对运行日志。

</details>

<details><summary>scripts/urdf-interaction-budget.test.cjs · node · 7 个命名用例</summary>

[源码入口](../../scripts/urdf-interaction-budget.test.cjs)

- `540 bounded nearest visible hit with actual r128 triangle intersections`
- `540 dense candidates and duplicate material groups reject before any triangle query`
- `540 off-ray dense bounds, invisible ancestors and transformed meshes`
- `542 real static PNG/JPEG/GIF/WebP lossy/lossless headers and animations`
- `542 dimensions rejected before raster decode; malformed headers fail boundedly`
- `542 inclusive per-preview pixel quota is shared and failure does not grow it`
- `542 actual URL modifier charges every decode request and substitutes neutral before loading`

</details>

<details><summary>scripts/urdf-preview.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/urdf-preview.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/workbench-background.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/workbench-background.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/workspace-scene-transition.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/workspace-scene-transition.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/xiaomi-model-picker.test.cjs · node · 0 个命名用例</summary>

[源码入口](../../scripts/xiaomi-model-picker.test.cjs)

- 未静态识别命名用例；可能是文件级断言、浏览器入口或测试辅助工具，需看实际执行回执。

</details>

<details><summary>scripts/xsys-review/unix_linux_test.go · go · 4 个命名用例</summary>

[源码入口](../../scripts/xsys-review/unix_linux_test.go)

- `TestIssue294DedupeCount`
- `TestIssue295CredentialsAndDestinationLengths`
- `TestIssue299ControlMessageHeader`
- `TestIssue296TeeByteCount`

</details>

<details><summary>scripts/xsys-review/windows_test.go · go · 3 个命名用例</summary>

[源码入口](../../scripts/xsys-review/windows_test.go)

- `TestIssue303WorkingSetFlags`
- `TestIssue302DescriptorFailures`
- `TestIssue305MissingPrivilege`

</details>

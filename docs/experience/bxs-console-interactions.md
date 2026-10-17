# BXS — Console interactive family migration

ADR-SHARED-040 §2.4. This increment migrates the owned modal, custom tabs,
command palette and legacy primitive button to pinned Base UI 1.9.0.

ConfirmDialog retains typed confirmation, required reason, pending state and
caller-owned command execution. Base UI owns focus isolation, Escape, portals
and focus restoration. Tabs explicitly activate on keyboard focus, retaining
previous behaviour. Command palette entries remain permission-filtered and
now expose actual links, preserving modified-click and open-in-new-tab.

Unit checks cover confirmation prerequisites, cancelled-form reset, focus
restoration, tab movement and restricted navigation. Browser interaction and
manual assistive-technology acceptance must be recorded separately; a passing
unit test or Shared Storybook is not protected Console workflow acceptance.
No CP authorisation, BFF, grant or provider activation boundary changes.

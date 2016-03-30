$(call setup-stamp-file,WIN32_ACI_STAMP,aci-manifest)
$(call setup-tmp-dir,WIN32_ACI_TMPDIR_BASE)

WIN32_ACI_TMPDIR := $(WIN32_ACI_TMPDIR_BASE)/win32
# a manifest template
WIN32_ACI_SRC_MANIFEST := $(MK_SRCDIR)/aci-manifest.in
# generated manifest to be copied to the ACI directory
WIN32_ACI_GEN_MANIFEST := $(WIN32_ACI_TMPDIR)/manifest
# manifest in the ACI directory
WIN32_ACI_MANIFEST := $(WIN32_ACIDIR)/manifest
# escaped values of the ACI name, version and enter command, so
# they can be safely used in the replacement part of sed's s///
# command.
WIN32_ACI_VERSION := $(call sed-replacement-escape,$(RKT_VERSION))
# stamp and dep file for invalidating the generated manifest if name,
# version or enter command changes for this flavor
$(call setup-stamp-file,WIN32_ACI_MANIFEST_KV_DEPMK_STAMP,$manifest-kv-dep)
$(call setup-dep-file,WIN32_ACI_MANIFEST_KV_DEPMK,manifest-kv-dep)
WIN32_ACI_DIRS := \
	$(WIN32_ACIROOTFSDIR)/rkt \
	$(WIN32_ACIROOTFSDIR)/rkt/status \
	$(WIN32_ACIROOTFSDIR)/opt \
	$(WIN32_ACIROOTFSDIR)/opt/stage2

# main stamp rule - makes sure manifest and deps files are generated
$(call generate-stamp-rule,$(WIN32_ACI_STAMP),$(WIN32_ACI_MANIFEST) $(WIN32_ACI_MANIFEST_KV_DEPMK_STAMP))

# invalidate generated manifest if version changes
$(call generate-kv-deps,$(WIN32_ACI_MANIFEST_KV_DEPMK_STAMP),$(WIN32_ACI_GEN_MANIFEST),$(WIN32_ACI_MANIFEST_KV_DEPMK),WIN32_ACI_VERSION)

# this rule generates a manifest
$(call forward-vars,$(WIN32_ACI_GEN_MANIFEST), \
	WIN32_ACI_VERSION)
$(WIN32_ACI_GEN_MANIFEST): $(WIN32_ACI_SRC_MANIFEST) | $(WIN32_ACI_TMPDIR) $(WIN32_ACI_DIRS) $(WIN32_ACIROOTFSDIR)/flavor
	$(VQ) \
	set -e; \
	$(call vb,vt,MANIFEST,win32) \
	sed \
		-e 's/@RKT_STAGE1_VERSION@/$(WIN32_ACI_VERSION)/g' \
	"$<" >"$@.tmp"; \
	$(call bash-cond-rename,$@.tmp,$@)

INSTALL_DIRS += \
	$(WIN32_ACI_TMPDIR):- \
	$(foreach d,$(WIN32_ACI_DIRS),$d:-)
INSTALL_SYMLINKS += \
	win32:$(WIN32_ACIROOTFSDIR)/flavor
WIN32_STAMPS += $(WIN32_ACI_STAMP)
INSTALL_FILES += \
	$(WIN32_ACI_GEN_MANIFEST):$(WIN32_ACI_MANIFEST):0644
CLEAN_FILES += $(WIN32_ACI_GEN_MANIFEST)

$(call undefine-namespaces,WIN32_ACI _WIN32_ACI)

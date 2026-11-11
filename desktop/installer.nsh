!macro customInstall
  CreateShortCut "$SMPROGRAMS\卸载 ${PRODUCT_NAME}.lnk" "$INSTDIR\${UNINSTALL_FILENAME}"
!macroend

!macro customUnInstall
  Delete "$SMPROGRAMS\卸载 ${PRODUCT_NAME}.lnk"
!macroend

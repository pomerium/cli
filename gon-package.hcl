sign {
  application_identity = "07205C57F24D7B05B457074E7E3B3823D411A101"
}

apple_id {
  username = "rsmith@pomerium.com"
  provider = "4RYVW695ZQ"
  # password is provided via AC_PASSWORD environment variable
}

notarize {
  path = "pomerium-cli.pkg"
  bundle_id = "com.pomerium.pomerium-cli"
  staple = true
}

sign {
  application_identity = "247F4DF4E206B054A93A737A7AFF1284F2C47F32"
}

apple_id {
  username = "bdd@pomerium.com"
  provider = "4RYVW695ZQ"
  # password is provided via AC_PASSWORD environment variable
}

notarize {
  path = "pomerium-cli.pkg"
  bundle_id = "com.pomerium.pomerium-cli"
  staple = true
}

source = ["bin/ARCH/pomerium-cli"]
bundle_id = "com.pomerium.pomerium-cli"

apple_id {
  username = "bdd@pomerium.com"
  provider = "4RYVW695ZQ"
# password is set via the AC_PASSWORD environment variable
}

sign {
  application_identity = "247F4DF4E206B054A93A737A7AFF1284F2C47F32"
}

zip {
  output_path = "pomerium-cli-darwin-ARCH.zip"
}

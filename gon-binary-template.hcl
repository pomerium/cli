source = ["bin/ARCH/pomerium-cli"]
bundle_id = "com.pomerium.pomerium-cli"

apple_id {
  username = "rsmith@pomerium.com"
  provider = "4RYVW695ZQ"
# password is set via the AC_PASSWORD environment variable
}

sign {
  application_identity = "07205C57F24D7B05B457074E7E3B3823D411A101"
}

zip {
  output_path = "pomerium-cli-darwin-ARCH.zip"
}

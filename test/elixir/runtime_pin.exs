[otp_want, elixir_want] = System.argv()

elixir_found = System.version()

otp_release = System.otp_release()

otp_version_path =
  Path.join([to_string(:code.root_dir()), "releases", otp_release, "OTP_VERSION"])

{otp_found, otp_ok} =
  case File.read(otp_version_path) do
    {:ok, contents} ->
      version = String.trim(contents)
      {version, version == otp_want}

    {:error, _reason} ->
      IO.puts(
        "FAILED: this gate certifies for OTP #{otp_want} but could not read the " <>
          "running OTP version from #{otp_version_path} (release #{otp_release}); " <>
          "make/elixir.mk names the pinned toolchain and where it comes from"
      )

      System.halt(1)
  end

elixir_ok = elixir_found == elixir_want

cond do
  System.get_env("SCHEMA_ELIXIR_ANY_OTP") == "1" ->
    IO.puts(
      :stderr,
      "NOT CERTIFIED: this gate is running OTP #{otp_found} (want #{otp_want}) " <>
        "and Elixir #{elixir_found} (want #{elixir_want}); SCHEMA_ELIXIR_ANY_OTP=1 " <>
        "reads the numbers without certifying them"
    )

  otp_ok and elixir_ok ->
    IO.puts("certified: OTP #{otp_found} with Elixir #{elixir_found}; OK")

  true ->
    IO.puts(
      "FAILED: this gate certifies for the pinned runtime (OTP #{otp_want}, " <>
        "Elixir #{elixir_want}); it is running OTP #{otp_found}, Elixir #{elixir_found}; " <>
        "make/elixir.mk names the pinned toolchain and where it comes from; " <>
        "SCHEMA_ELIXIR_ANY_OTP=1 reads the numbers without certifying them"
    )

    System.halt(1)
end

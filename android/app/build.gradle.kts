plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}

android {
    namespace = "com.nepulse.sensor"
    compileSdk = 35

    defaultConfig {
        applicationId = "com.nepulse.sensor"
        minSdk = 24 // Android 7: old phones from a drawer are the best sensors
        targetSdk = 35
        versionCode = (System.getenv("NEPULSE_VERSION_CODE") ?: "1").toInt()
        versionName = System.getenv("NEPULSE_VERSION_NAME") ?: "1.0.0"
    }

    // A fixed key so every new build installs over the previous one. The app
    // is installed from ne-pulse.com, not a store; see android/README.md.
    signingConfigs {
        create("release") {
            storeFile = file("../signing/ne-pulse-sideload.jks")
            storePassword = "nepulse-sideload"
            keyAlias = "nepulse"
            keyPassword = "nepulse-sideload"
        }
    }

    buildTypes {
        release {
            isMinifyEnabled = true
            isShrinkResources = true
            proguardFiles(getDefaultProguardFile("proguard-android-optimize.txt"), "proguard-rules.pro")
            signingConfig = signingConfigs.getByName("release")
        }
        debug {
            signingConfig = signingConfigs.getByName("release")
        }
    }

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlinOptions { jvmTarget = "17" }
    buildFeatures { buildConfig = true }
    lint {
        checkReleaseBuilds = false
        abortOnError = false
    }
}

dependencies {
    implementation("com.squareup.okhttp3:okhttp:4.12.0")
}

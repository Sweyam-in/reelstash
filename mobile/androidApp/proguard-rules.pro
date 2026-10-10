# Keep kotlinx.serialization metadata for the shared models.
-keepattributes *Annotation*, InnerClasses
-keep,includedescriptorclasses class app.reelstash.shared.**$$serializer { *; }
-keepclassmembers class app.reelstash.shared.** { *** Companion; }
-keepclasseswithmembers class app.reelstash.shared.** { kotlinx.serialization.KSerializer serializer(...); }

import ExpoModulesCore

public class PileupCoreModule: Module {
  public func definition() -> ModuleDefinition {
    Name("PileupCore")

    Function("hello") {
      return "Hello world! 👋"
    }

    AsyncFunction("setValueAsync") { (value: String) in
    }
  }
}

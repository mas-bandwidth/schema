import java.io.BufferedReader;
import java.io.InputStreamReader;
import java.util.Arrays;
import java.util.HexFormat;
import java.util.function.Function;
import java.util.function.ToIntFunction;
import java.util.function.ToIntBiFunction;
import wide.WideText;
import wideprobe.Shapes;

public final class Main {
    interface Reader<T> { boolean read(T v, byte[] wire, int bits); }
    static void check(boolean ok,String message) { if(!ok) throw new AssertionError(message); }
    static <T> void replay(byte[] wire,T v,Reader<T> read,ToIntBiFunction<T,byte[]> write,ToIntFunction<T> measure,Function<T,char[]> text,ToIntFunction<T> length) {
        Arrays.fill(text.apply(v),(char)0x7f7f);
        if(!read.read(v,wire,wire.length*8)) { System.out.println("REFUSE"); return; }
        int bits=measure.applyAsInt(v);
        check(read.read(v,wire,bits),"exact bit read");
        byte[] encoded=new byte[256];
        int written=write.applyAsInt(v,encoded);
        check(written==(bits+7)/8,"writer byte count");
        StringBuilder payload=new StringBuilder();
        for(int i=0;i<length.applyAsInt(v);i++) payload.append(String.format("%04x",(int)text.apply(v)[i]));
        if(payload.length()==0) payload.append('-');
        check(!read.read(v,wire,bits-1),"one bit short read");
        System.out.printf("OK %d %s %d %s%n",bits,payload,bits,HexFormat.of().formatHex(encoded,0,written));
    }
    static void contracts() {
        var v=new WideText.WideSeven();
        byte[] wire=new byte[1024];
        for(int n:new int[]{-1,8,1}) {
            v.textLength=n; boolean refused=false;
            try { WideText.writeWideSeven(v,wire); } catch(IllegalArgumentException|AssertionError e) { refused=true; }
            check(refused,"writer length/null");
        }
        v.text[0]=(char)0xd800;
        WideText.writeWideSeven(v,wire);
        check(!WideText.readWideSeven(v,wire,35),"unpaired high");
        v.text[0]=(char)0xffff;
        WideText.writeWideSeven(v,wire);
        Arrays.fill(v.text,(char)0x7f7f);
        check(WideText.readWideSeven(v,wire,35) && v.text[0]==0xffff && v.text[1]==0x7f7f && v.text[6]==0x7f7f,"tail");
        var c=new Shapes.Conditional(); c.enabled=true; c.textLength=2;
        c.text[0]=(char)0xd800; c.text[1]=(char)0xdc00;
        int bits=Shapes.measureConditional(c);
        Shapes.writeConditional(c,wire);
        check(bits==68 && (wire[0]&15)==5,"unaligned groups");
        var co=new Shapes.Conditional();
        check(Shapes.readConditional(co,wire,bits) && co.textLength==2 && co.text[1]==0xdc00,"conditional");
        c.enabled=false; Shapes.writeConditional(c,wire);
        check(Shapes.readConditional(co,wire,1) && co.textLength==0 && co.text[0]==0 && co.text[1]==0,"branch zero");
        var choice=new Shapes.Choice(); choice.type=Shapes.ChoiceType.text;
        choice.text.valueLength=1; choice.text.value[0]=(char)0xffff;
        Shapes.writeChoice(choice,wire); bits=Shapes.measureChoice(choice);
        var out=new Shapes.Choice();
        for(int i=0;i<2;i++) { out.text.value[3]=(char)0x7f7f; check(Shapes.readChoice(out,wire,bits) && out.text.valueLength==1 && out.text.value[0]==0xffff && out.text.value[3]==0,"union reset"); }
        var box=new Shapes.Box(); check(box.countedCount==1,"born count");
        box.items[0].valueLength=1; box.items[0].value[0]=(char)0xffff;
        box.counted[0].valueLength=1; box.counted[0].value[0]='A';
        box.choice.type=Shapes.ChoiceType.text;
        Shapes.writeBox(box,wire); bits=Shapes.measureBox(box);
        var bo=new Shapes.Box(); bo.counted[1].value[0]=(char)0x7f7f;
        check(Shapes.readBox(bo,wire,bits) && bo.items[0].value[0]==0xffff && bo.countedCount==1 && bo.counted[0].value[0]=='A' && bo.counted[1].value[0]==0x7f7f && bo.choice.type==Shapes.ChoiceType.text,"composition");
        check(!Shapes.readBox(bo,wire,bits-1),"composition truncation");
    }
    public static void main(String[] args) throws Exception {
        if(args.length!=0) { contracts(); return; }
        var input=new BufferedReader(new InputStreamReader(System.in));
        String line;
        while((line=input.readLine())!=null) {
            String[] parts=line.split(" ");
            byte[] wire=parts[1].equals("-")?new byte[0]:HexFormat.of().parseHex(parts[1]);
            switch(parts[0]) {
                case "7": replay(wire,new WideText.WideSeven(),WideText::readWideSeven,WideText::writeWideSeven,WideText::measureWideSeven,v->v.text,v->v.textLength); break;
                case "4": replay(wire,new WideText.WideFour(),WideText::readWideFour,WideText::writeWideFour,WideText::measureWideFour,v->v.text,v->v.textLength); break;
                default: throw new AssertionError("unexpected bound");
            }
        }
    }
}
